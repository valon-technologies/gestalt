import io
import logging
import os
import unittest
from typing import Any, cast
from unittest.mock import MagicMock, patch

import opentelemetry._logs._internal as _logs_internal
import opentelemetry.metrics._internal as _metrics_internal
from opentelemetry import metrics as otel_metrics
from opentelemetry import trace as otel_trace
from opentelemetry._logs import get_logger_provider
from opentelemetry.sdk._logs import LoggerProvider, LoggingHandler
from opentelemetry.sdk._logs.export import (
    BatchLogRecordProcessor,
    InMemoryLogRecordExporter,
)
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import (
    MetricExporter,
    MetricExportResult,
    MetricsData,
)
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider

from gestalt import _telemetry, telemetry
from gestalt._telemetry import _configure_logging as _real_configure_logging


class NoOpMetricExporter(MetricExporter):
    """MetricExporter that keeps tests from trying to reach a real OTLP collector."""

    def __init__(self) -> None:
        super().__init__()

    def export(
        self,
        metrics_data: MetricsData,
        timeout_millis: float = 10_000,
        **kwargs: Any,
    ) -> MetricExportResult:
        _ = (metrics_data, timeout_millis, kwargs)
        return MetricExportResult.SUCCESS

    def force_flush(self, timeout_millis: float = 10_000) -> bool:
        _ = timeout_millis
        return True

    def shutdown(self, timeout_millis: float = 30_000, **kwargs: Any) -> None:
        _ = (timeout_millis, kwargs)
        return None


class TelemetryImportTests(unittest.TestCase):
    def test_telemetry_is_public_sdk_module(self) -> None:
        self.assertEqual(telemetry.GENAI_OPERATION_CHAT, "chat")
        self.assertTrue(callable(telemetry.model_operation))

    def test_model_operation_keeps_metric_attrs_low_cardinality(self) -> None:
        operation = telemetry.model_operation(
            provider_name="openai",
            request_model="gpt-4.1",
            request_options={"seed": 123, "temperature": 0.2},
            request_attrs={"tenant.id": "tenant-123"},
        )

        self.assertIn("gen_ai.request.seed", operation._span_attrs)
        self.assertIn("tenant.id", operation._span_attrs)
        self.assertNotIn("gen_ai.request.seed", operation._metric_attrs)
        self.assertNotIn("tenant.id", operation._metric_attrs)

    def test_operation_records_exceptions_itself(self) -> None:
        span_context = MagicMock()
        tracer = MagicMock()
        tracer.start_as_current_span.return_value = span_context

        with patch("gestalt._telemetry.trace.get_tracer", return_value=tracer):
            with self.assertRaises(RuntimeError):
                with telemetry.model_operation(provider_name="openai", request_model="gpt-4.1"):
                    raise RuntimeError("model failed")

        _, kwargs = tracer.start_as_current_span.call_args
        self.assertFalse(kwargs["record_exception"])
        self.assertFalse(kwargs["set_status_on_exception"])


class OTLPLoggingTests(unittest.TestCase):
    def setUp(self) -> None:
        self._original_env = {
            "OTEL_EXPORTER_OTLP_ENDPOINT": os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT"),
            "OTEL_EXPORTER_OTLP_PROTOCOL": os.environ.get("OTEL_EXPORTER_OTLP_PROTOCOL"),
            "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT": os.environ.get("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"),
            "OTEL_RESOURCE_ATTRIBUTES": os.environ.get("OTEL_RESOURCE_ATTRIBUTES"),
            "GESTALT_PROVIDER_LOG_LEVEL": os.environ.get("GESTALT_PROVIDER_LOG_LEVEL"),
            "OTEL_SDK_DISABLED": os.environ.get("OTEL_SDK_DISABLED"),
        }
        os.environ["OTEL_EXPORTER_OTLP_ENDPOINT"] = "http://localhost:4317"
        os.environ["OTEL_EXPORTER_OTLP_PROTOCOL"] = "grpc"
        os.environ["OTEL_RESOURCE_ATTRIBUTES"] = "deployment.environment=test"
        os.environ.pop("OTEL_SDK_DISABLED", None)

        self._added_handlers: list[logging.Handler] = []
        self._swap_exporters()
        self._reset_telemetry()

    def tearDown(self) -> None:
        for key, value in self._original_env.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value

        self._reset_telemetry()
        for handler in self._added_handlers:
            logging.getLogger().removeHandler(handler)
        self._added_handlers.clear()

    def _reset_telemetry(self) -> None:
        """Flush any configured providers, then reset module flags and OTEL globals so tests stay isolated."""
        _telemetry.shutdown()
        _telemetry._configured = False
        _telemetry._atexit_registered = False
        _telemetry._logger_provider = None
        logging.getLogger().setLevel(logging.WARNING)

        otel_trace._TRACER_PROVIDER = None
        otel_trace._TRACER_PROVIDER_SET_ONCE._done = False
        _metrics_internal._METER_PROVIDER = None
        _metrics_internal._METER_PROVIDER_SET_ONCE._done = False
        _logs_internal._LOGGER_PROVIDER = None
        _logs_internal._LOGGER_PROVIDER_SET_ONCE._done = False

    def _swap_exporters(self) -> None:
        """Replace OTLP metric and log exporters with in-memory/no-op versions for testing."""
        self._log_exporter = InMemoryLogRecordExporter()

        original_metric_exporter = _telemetry._metric_exporter
        original_configure_logging = _telemetry._configure_logging

        def metric_exporter() -> NoOpMetricExporter:
            return NoOpMetricExporter()

        def configure_logging_with_in_memory(resource: Resource) -> None:
            logger_provider = LoggerProvider(resource=resource)
            logger_provider.add_log_record_processor(
                BatchLogRecordProcessor(self._log_exporter)
            )
            _telemetry._logger_provider = logger_provider
            _telemetry.set_logger_provider(logger_provider)
            handler = LoggingHandler(
                level=_telemetry._log_level(), logger_provider=logger_provider
            )
            logging.getLogger().addHandler(handler)
            self._added_handlers.append(handler)

        _telemetry._metric_exporter = cast(Any, metric_exporter)
        _telemetry._configure_logging = cast(Any, configure_logging_with_in_memory)

        self.addCleanup(
            lambda: setattr(_telemetry, "_metric_exporter", original_metric_exporter)
        )
        self.addCleanup(
            lambda: setattr(_telemetry, "_configure_logging", original_configure_logging)
        )

    def _force_flush_logs(self) -> None:
        logger_provider = _telemetry._logger_provider
        self.assertIsInstance(logger_provider, LoggerProvider)
        assert isinstance(logger_provider, LoggerProvider)
        logger_provider.force_flush()

    def test_configure_from_environment_exports_log_records(self) -> None:
        os.environ.pop("GESTALT_PROVIDER_LOG_LEVEL", None)

        _telemetry.configure_from_environment(service_name="test-provider")

        logger = logging.getLogger("test-provider")
        logger.setLevel(logging.INFO)
        logger.info("test message")
        self._force_flush_logs()

        records = self._log_exporter.get_finished_logs()
        self.assertEqual(len(records), 1)
        self.assertEqual(records[0].log_record.body, "test message")

        logger_provider = _telemetry._logger_provider
        assert isinstance(logger_provider, LoggerProvider)
        resource_attrs = dict(logger_provider.resource.attributes)
        self.assertIn("service.name", resource_attrs)
        self.assertEqual(resource_attrs["service.name"], "test-provider")
        self.assertIn("deployment.environment", resource_attrs)

    def test_configure_from_environment_skips_when_otel_disabled(self) -> None:
        os.environ["OTEL_SDK_DISABLED"] = "true"
        _telemetry.configure_from_environment()
        provider = get_logger_provider()
        self.assertNotIsInstance(provider, LoggerProvider)


    def test_log_level_env_controls_handler_level(self) -> None:
        os.environ["GESTALT_PROVIDER_LOG_LEVEL"] = "DEBUG"
        _telemetry.configure_from_environment(service_name="level-test")
        logger = logging.getLogger("level-test")
        logger.setLevel(logging.DEBUG)
        logger.debug("debug message")
        self._force_flush_logs()

        records = self._log_exporter.get_finished_logs()
        self.assertEqual(len(records), 1)
        self.assertEqual(records[0].log_record.body, "debug message")

    def test_shutdown_flushes_log_provider(self) -> None:
        os.environ.pop("GESTALT_PROVIDER_LOG_LEVEL", None)

        _telemetry.configure_from_environment()
        logging.getLogger().error("shutdown test")

        _telemetry.shutdown()

        records = self._log_exporter.get_finished_logs()
        self.assertEqual(len(records), 1)
        self.assertEqual(records[0].log_record.body, "shutdown test")

    def test_configure_from_environment_registers_shutdown_when_logging_fails(self) -> None:
        """Trace/metric providers are configured even if logging setup fails."""
        # Use the real _configure_logging so we can patch it to raise.
        original_configure_logging = _telemetry._configure_logging
        _telemetry._configure_logging = cast(Any, _real_configure_logging)

        failing_configure = MagicMock(side_effect=RuntimeError("logging failed"))
        _telemetry._configure_logging = cast(Any, failing_configure)
        self.addCleanup(
            lambda: setattr(_telemetry, "_configure_logging", original_configure_logging)
        )

        _telemetry.configure_from_environment(service_name="orphan-test")

        self.assertTrue(_telemetry._configured)
        self.assertTrue(_telemetry._atexit_registered)
        tracer_provider = otel_trace.get_tracer_provider()
        self.assertIsInstance(tracer_provider, TracerProvider)
        meter_provider = otel_metrics.get_meter_provider()
        self.assertIsInstance(meter_provider, MeterProvider)

    def test_configure_from_environment_adds_stderr_stream_handler(self) -> None:
        """A StreamHandler is attached to the root logger and emits to stderr."""
        os.environ.pop("GESTALT_PROVIDER_LOG_LEVEL", None)

        # Replace the in-memory _configure_logging with the real implementation
        # and patch the OTLP log exporter so it does not try to reach a real collector.
        original_configure_logging = _telemetry._configure_logging
        original_log_exporter = _telemetry._log_exporter
        _telemetry._configure_logging = cast(Any, _real_configure_logging)
        _telemetry._log_exporter = cast(Any, lambda: InMemoryLogRecordExporter())
        self.addCleanup(lambda: setattr(_telemetry, "_configure_logging", original_configure_logging))
        self.addCleanup(lambda: setattr(_telemetry, "_log_exporter", original_log_exporter))

        # Reset telemetry state so configure_from_environment runs fresh.
        _telemetry._configured = False
        _telemetry._atexit_registered = False

        _telemetry.configure_from_environment(service_name="stderr-test")

        root = logging.getLogger()
        stream_handlers = [h for h in root.handlers if isinstance(h, logging.StreamHandler) and not h.__class__.__name__.startswith("_") and h.__class__.__name__ != "LogCaptureHandler"]
        self.assertTrue(stream_handlers, "expected a StreamHandler on the root logger")
        handler = stream_handlers[0]
        self.assertEqual(handler.level, logging.INFO)
        self.assertIsNotNone(handler.formatter)

        logger = logging.getLogger("stderr-test")
        logger.setLevel(logging.INFO)

        original_stream = handler.stream
        captured = io.StringIO()
        handler.stream = captured
        self.addCleanup(lambda: setattr(handler, "stream", original_stream))

        logger.info("stderr message")

        emitted = captured.getvalue()
        self.assertIn("stderr message", emitted)
        self.assertIn("INFO", emitted)
        self.assertIn("stderr-test", emitted)


    def test_configure_from_environment_sets_root_logger_level(self) -> None:
        """The root logger level is set to GESTALT_PROVIDER_LOG_LEVEL."""
        os.environ.pop("GESTALT_PROVIDER_LOG_LEVEL", None)
        original_configure_logging = _telemetry._configure_logging
        original_log_exporter = _telemetry._log_exporter
        _telemetry._configure_logging = cast(Any, _real_configure_logging)
        _telemetry._log_exporter = cast(Any, lambda: InMemoryLogRecordExporter())
        self.addCleanup(lambda: setattr(_telemetry, "_configure_logging", original_configure_logging))
        self.addCleanup(lambda: setattr(_telemetry, "_log_exporter", original_log_exporter))
        _telemetry._configured = False
        _telemetry._atexit_registered = False

        _telemetry.configure_from_environment(service_name="root-level-test")
        self.assertEqual(logging.getLogger().level, logging.INFO)

        os.environ["GESTALT_PROVIDER_LOG_LEVEL"] = "DEBUG"
        _telemetry._configured = False
        _telemetry._atexit_registered = False
        _telemetry.configure_from_environment(service_name="root-level-debug-test")
        self.assertEqual(logging.getLogger().level, logging.DEBUG)

    def test_configure_from_environment_exports_logs_when_traces_metrics_fail(self) -> None:
        """Logging is configured even if trace and metric setup fail."""
        original_trace_exporter = _telemetry._trace_exporter
        original_metric_exporter = _telemetry._metric_exporter
        original_configure_logging = _telemetry._configure_logging
        original_log_exporter = _telemetry._log_exporter

        def failing_trace_exporter() -> Any:
            raise RuntimeError("trace failed")

        def failing_metric_exporter() -> Any:
            raise RuntimeError("metric failed")

        _telemetry._configure_logging = cast(Any, _real_configure_logging)
        _telemetry._log_exporter = cast(Any, lambda: self._log_exporter)
        _telemetry._trace_exporter = cast(Any, failing_trace_exporter)
        _telemetry._metric_exporter = cast(Any, failing_metric_exporter)
        self.addCleanup(lambda: setattr(_telemetry, "_trace_exporter", original_trace_exporter))
        self.addCleanup(lambda: setattr(_telemetry, "_metric_exporter", original_metric_exporter))
        self.addCleanup(lambda: setattr(_telemetry, "_configure_logging", original_configure_logging))
        self.addCleanup(lambda: setattr(_telemetry, "_log_exporter", original_log_exporter))

        _telemetry._configured = False
        _telemetry._atexit_registered = False
        _telemetry.configure_from_environment(service_name="logs-only-test")

        self.assertTrue(_telemetry._configured)
        logger = logging.getLogger("logs-only-test")
        logger.info("logs-only message")
        self._force_flush_logs()

        records = self._log_exporter.get_finished_logs()
        bodies = [record.log_record.body for record in records]
        self.assertIn("logs-only message", bodies)



if __name__ == "__main__":
    unittest.main()
