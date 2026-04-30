from __future__ import annotations

import atexit
import os
import time
import urllib.parse
from collections.abc import Mapping
from types import TracebackType
from typing import Any

from opentelemetry import metrics, trace
from opentelemetry.metrics import Histogram
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.trace import Span, SpanKind, Status, StatusCode

INSTRUMENTATION_NAME = "gestalt.provider"
GENAI_PROVIDER_NAME = "gestalt"
GENAI_OPERATION_CHAT = "chat"
GENAI_OPERATION_EXECUTE_TOOL = "execute_tool"
GENAI_OPERATION_INVOKE_AGENT = "invoke_agent"
GENAI_TOOL_TYPE_DATASTORE = "datastore"
GENAI_TOOL_TYPE_EXTENSION = "extension"

_OPERATION_DURATION_BUCKETS = (0.01, 0.02, 0.04, 0.08, 0.16, 0.32, 0.64, 1.28, 2.56, 5.12, 10.24, 20.48, 40.96, 81.92)
_TOKEN_USAGE_BUCKETS = (1, 4, 16, 64, 256, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864)
_configured = False
_atexit_registered = False
_operation_duration: Histogram | None = None
_token_usage: Histogram | None = None


def configure_from_environment(*, service_name: str = "gestalt-provider") -> None:
    """Configure OpenTelemetry exporters from standard OTEL environment variables."""

    global _atexit_registered, _configured
    if _configured or _otel_disabled() or not _otel_export_enabled():
        return

    resource = Resource.create(_resource_attributes(service_name))
    try:
        tracer_provider = TracerProvider(resource=resource)
        tracer_provider.add_span_processor(BatchSpanProcessor(_trace_exporter()))
        trace.set_tracer_provider(tracer_provider)

        metric_reader = PeriodicExportingMetricReader(_metric_exporter())
        metrics.set_meter_provider(MeterProvider(resource=resource, metric_readers=[metric_reader]))
    except Exception:
        return

    _configured = True
    if not _atexit_registered:
        atexit.register(shutdown)
        _atexit_registered = True


def shutdown() -> None:
    """Flush and shut down configured OpenTelemetry providers."""

    _shutdown_provider(trace.get_tracer_provider())
    _shutdown_provider(metrics.get_meter_provider())


def model_operation(
    *,
    provider_name: str,
    request_model: str,
    request_options: Mapping[str, Any] | None = None,
    request_attrs: Mapping[str, Any] | None = None,
) -> Operation:
    """Create a GenAI chat/model operation around an upstream model SDK call."""

    provider_name = _clean(provider_name) or "_OTHER"
    request_model = _clean(request_model)
    attrs = {
        "gen_ai.operation.name": GENAI_OPERATION_CHAT,
        "gen_ai.provider.name": provider_name,
        "gen_ai.request.model": request_model,
    }
    span_attrs = dict(attrs)
    span_attrs.update(_request_option_attrs(request_options))
    span_attrs.update(_clean_attrs(request_attrs))
    return Operation(
        span_name=f"{GENAI_OPERATION_CHAT} {request_model}".strip(),
        span_kind=SpanKind.CLIENT,
        span_attrs=span_attrs,
        metric_attrs=attrs,
    )


def agent_invocation(*, agent_name: str, session_id: str, turn_id: str, model: str) -> Operation:
    """Create a GenAI agent invocation operation for a provider-owned turn."""

    agent_name = _clean(agent_name) or "provider"
    span_attrs = {
        "gen_ai.operation.name": GENAI_OPERATION_INVOKE_AGENT,
        "gen_ai.provider.name": GENAI_PROVIDER_NAME,
        "gen_ai.agent.name": agent_name,
        "gen_ai.conversation.id": _clean(session_id),
        "gen_ai.request.model": _clean(model),
        "gestalt.agent.turn_id": _clean(turn_id),
    }
    metric_attrs = {
        "gen_ai.operation.name": GENAI_OPERATION_INVOKE_AGENT,
        "gen_ai.provider.name": GENAI_PROVIDER_NAME,
        "gen_ai.agent.name": agent_name,
        "gen_ai.request.model": _clean(model),
    }
    return Operation(
        span_name=f"{GENAI_OPERATION_INVOKE_AGENT} {agent_name}",
        span_kind=SpanKind.INTERNAL,
        span_attrs=span_attrs,
        metric_attrs=metric_attrs,
    )


def tool_execution(*, tool_name: str, tool_call_id: str = "", tool_type: str = GENAI_TOOL_TYPE_EXTENSION) -> Operation:
    """Create a GenAI tool execution operation."""

    tool_name = _clean(tool_name) or "_OTHER"
    span_attrs = {
        "gen_ai.operation.name": GENAI_OPERATION_EXECUTE_TOOL,
        "gen_ai.tool.name": tool_name,
        "gen_ai.tool.call.id": _clean(tool_call_id),
        "gen_ai.tool.type": _clean(tool_type) or GENAI_TOOL_TYPE_EXTENSION,
        "gen_ai.provider.name": GENAI_PROVIDER_NAME,
    }
    metric_attrs = {
        "gen_ai.operation.name": GENAI_OPERATION_EXECUTE_TOOL,
        "gen_ai.provider.name": GENAI_PROVIDER_NAME,
        "gen_ai.tool.name": tool_name,
        "gen_ai.tool.type": _clean(tool_type) or GENAI_TOOL_TYPE_EXTENSION,
    }
    return Operation(
        span_name=f"{GENAI_OPERATION_EXECUTE_TOOL} {tool_name}",
        span_kind=SpanKind.INTERNAL,
        span_attrs=span_attrs,
        metric_attrs=metric_attrs,
    )


class Operation:
    """Context manager that records a GenAI span plus duration and token metrics."""

    def __init__(
        self,
        *,
        span_name: str,
        span_kind: SpanKind,
        span_attrs: Mapping[str, Any],
        metric_attrs: Mapping[str, Any],
    ) -> None:
        self._span_name = span_name
        self._span_kind = span_kind
        self._span_attrs = _clean_attrs(span_attrs)
        self._metric_attrs = _clean_attrs(metric_attrs)
        self._span_cm: Any = None
        self._span: Span | None = None
        self._started_at = 0.0
        self._error_type = ""

    def __enter__(self) -> Operation:
        self._started_at = time.perf_counter()
        tracer = trace.get_tracer(INSTRUMENTATION_NAME)
        self._span_cm = tracer.start_as_current_span(
            self._span_name,
            kind=self._span_kind,
            attributes=self._span_attrs,
            record_exception=False,
            set_status_on_exception=False,
        )
        self._span = self._span_cm.__enter__()
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        traceback_value: TracebackType | None,
    ) -> bool:
        del traceback_value
        metric_attrs = dict(self._metric_attrs)
        if exc is not None:
            error_type = _error_type(exc)
            self.mark_error(error_type, str(exc), exc=exc)
        if self._error_type:
            metric_attrs["error.type"] = self._error_type
        _record_operation_duration(time.perf_counter() - self._started_at, metric_attrs)
        if self._span_cm is not None:
            self._span_cm.__exit__(exc_type, exc, None)
        return False

    def mark_error(self, error_type: str, description: str = "", *, exc: BaseException | None = None) -> None:
        """Mark the operation span and duration metric as failed."""

        self._error_type = _clean(error_type) or "_OTHER"
        self.set_attribute("error.type", self._error_type)
        if self._span is None:
            return
        if exc is not None:
            self._span.record_exception(exc)
        self._span.set_status(Status(StatusCode.ERROR, description or self._error_type))

    def set_attribute(self, key: str, value: Any) -> None:
        """Set a span attribute when the value is valid for OpenTelemetry."""

        if self._span is None or not _valid_attr(value):
            return
        self._span.set_attribute(key, value)
        if key == "gen_ai.response.model":
            self._metric_attrs[key] = value

    def set_response_metadata(self, response: Any, *, finish_reasons: list[str] | None = None) -> None:
        """Attach common model response metadata to the current span."""

        self.set_attribute("gen_ai.response.id", _attr_from_object(response, "id"))
        self.set_attribute("gen_ai.response.model", _attr_from_object(response, "model"))
        if finish_reasons:
            self.set_attribute("gen_ai.response.finish_reasons", finish_reasons)

    def record_usage(
        self,
        *,
        input_tokens: Any = None,
        output_tokens: Any = None,
        cache_creation_input_tokens: Any = None,
        cache_read_input_tokens: Any = None,
        reasoning_output_tokens: Any = None,
    ) -> None:
        """Record token usage on the span and token-usage metric."""

        input_count = _int_or_none(input_tokens)
        output_count = _int_or_none(output_tokens)
        cache_creation = _int_or_none(cache_creation_input_tokens)
        cache_read = _int_or_none(cache_read_input_tokens)
        reasoning_output = _int_or_none(reasoning_output_tokens)

        self.set_attribute("gen_ai.usage.input_tokens", input_count)
        self.set_attribute("gen_ai.usage.output_tokens", output_count)
        self.set_attribute("gen_ai.usage.cache_creation.input_tokens", cache_creation)
        self.set_attribute("gen_ai.usage.cache_read.input_tokens", cache_read)
        self.set_attribute("gen_ai.usage.reasoning.output_tokens", reasoning_output)

        if input_count is not None:
            _record_token_usage(input_count, {**self._metric_attrs, "gen_ai.token.type": "input"})
        if output_count is not None:
            _record_token_usage(output_count, {**self._metric_attrs, "gen_ai.token.type": "output"})


def record_openai_usage(operation: Operation, response: Any) -> None:
    """Record token usage fields from an OpenAI SDK response."""

    usage = _attr_from_object(response, "usage")
    if usage is None:
        return
    output_details = _attr_from_object(usage, "completion_tokens_details") or _attr_from_object(
        usage, "output_tokens_details"
    )
    operation.record_usage(
        input_tokens=_first_attr(usage, "prompt_tokens", "input_tokens"),
        output_tokens=_first_attr(usage, "completion_tokens", "output_tokens"),
        reasoning_output_tokens=_attr_from_object(output_details, "reasoning_tokens"),
    )


def record_anthropic_usage(operation: Operation, response: Any) -> None:
    """Record token usage fields from an Anthropic SDK response."""

    usage = _attr_from_object(response, "usage")
    if usage is None:
        return
    operation.record_usage(
        input_tokens=_attr_from_object(usage, "input_tokens"),
        output_tokens=_attr_from_object(usage, "output_tokens"),
        cache_creation_input_tokens=_attr_from_object(usage, "cache_creation_input_tokens"),
        cache_read_input_tokens=_attr_from_object(usage, "cache_read_input_tokens"),
    )


def _record_operation_duration(duration_seconds: float, attrs: Mapping[str, Any]) -> None:
    histogram = _operation_duration_histogram()
    if histogram is None:
        return
    histogram.record(max(0.0, duration_seconds), attributes=_clean_attrs(attrs))


def _record_token_usage(tokens: int, attrs: Mapping[str, Any]) -> None:
    histogram = _token_usage_histogram()
    if histogram is None:
        return
    histogram.record(tokens, attributes=_clean_attrs(attrs))


def _operation_duration_histogram() -> Histogram | None:
    global _operation_duration
    if _operation_duration is None:
        meter = metrics.get_meter(INSTRUMENTATION_NAME)
        _operation_duration = meter.create_histogram(
            "gen_ai.client.operation.duration",
            unit="s",
            description="GenAI operation duration.",
            explicit_bucket_boundaries_advisory=list(_OPERATION_DURATION_BUCKETS),
        )
    return _operation_duration


def _token_usage_histogram() -> Histogram | None:
    global _token_usage
    if _token_usage is None:
        meter = metrics.get_meter(INSTRUMENTATION_NAME)
        _token_usage = meter.create_histogram(
            "gen_ai.client.token.usage",
            unit="{token}",
            description="Number of input and output tokens used.",
            explicit_bucket_boundaries_advisory=list(_TOKEN_USAGE_BUCKETS),
        )
    return _token_usage


def _trace_exporter() -> Any:
    if _otel_protocol().startswith("http"):
        from opentelemetry.exporter.otlp.proto.http.trace_exporter import (
            OTLPSpanExporter,
        )

        return OTLPSpanExporter()

    from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter

    return OTLPSpanExporter()


def _metric_exporter() -> Any:
    if _otel_protocol().startswith("http"):
        from opentelemetry.exporter.otlp.proto.http.metric_exporter import (
            OTLPMetricExporter,
        )

        return OTLPMetricExporter()

    from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import (
        OTLPMetricExporter,
    )

    return OTLPMetricExporter()


def _resource_attributes(service_name: str) -> dict[str, str]:
    attrs = _resource_attrs_from_env()
    attrs["service.name"] = os.getenv("OTEL_SERVICE_NAME", service_name)
    return attrs


def _resource_attrs_from_env() -> dict[str, str]:
    raw = os.getenv("OTEL_RESOURCE_ATTRIBUTES", "")
    attrs: dict[str, str] = {}
    for part in raw.split(","):
        key, sep, value = part.partition("=")
        key = urllib.parse.unquote(key.strip())
        value = urllib.parse.unquote(value.strip())
        if sep and key and value:
            attrs[key] = value
    return attrs


def _request_option_attrs(options: Mapping[str, Any] | None) -> dict[str, Any]:
    if not options:
        return {}
    return _clean_attrs(
        {
            "gen_ai.request.choice.count": _first_present(options, "n", "candidate_count"),
            "gen_ai.request.frequency_penalty": options.get("frequency_penalty"),
            "gen_ai.request.max_tokens": _first_present(options, "max_tokens", "max_completion_tokens", "max_output_tokens"),
            "gen_ai.request.presence_penalty": options.get("presence_penalty"),
            "gen_ai.request.seed": options.get("seed"),
            "gen_ai.request.temperature": options.get("temperature"),
            "gen_ai.request.top_k": options.get("top_k"),
            "gen_ai.request.top_p": options.get("top_p"),
        }
    )


def _first_present(values: Mapping[str, Any], *keys: str) -> Any:
    for key in keys:
        if key in values:
            return values[key]
    return None


def _clean_attrs(attrs: Mapping[str, Any] | None) -> dict[str, Any]:
    if not attrs:
        return {}
    return {key: value for key, value in attrs.items() if key and _valid_attr(value)}


def _valid_attr(value: Any) -> bool:
    if value is None:
        return False
    if isinstance(value, str):
        return bool(value.strip())
    if isinstance(value, (bool, int, float)):
        return True
    if isinstance(value, (list, tuple)):
        return all(isinstance(item, str) and item.strip() for item in value)
    return False


def _clean(value: Any) -> str:
    return str(value or "").strip()


def _attr_from_object(value: Any, name: str) -> Any:
    if value is None:
        return None
    if isinstance(value, Mapping):
        return value.get(name)
    return getattr(value, name, None)


def _first_attr(value: Any, *names: str) -> Any:
    for name in names:
        candidate = _attr_from_object(value, name)
        if candidate is not None:
            return candidate
    return None


def _int_or_none(value: Any) -> int | None:
    if isinstance(value, bool) or value is None:
        return None
    try:
        integer = int(value)
    except (TypeError, ValueError):
        return None
    return integer if integer >= 0 else None


def _error_type(exc: BaseException) -> str:
    return exc.__class__.__name__ or "_OTHER"


def _otel_protocol() -> str:
    return os.getenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc").strip().lower()


def _otel_disabled() -> bool:
    return os.getenv("OTEL_SDK_DISABLED", "").strip().lower() in {"1", "true", "yes"}


def _otel_export_enabled() -> bool:
    return any(
        os.getenv(name)
        for name in (
            "OTEL_EXPORTER_OTLP_ENDPOINT",
            "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
            "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
        )
    )


def _shutdown_provider(provider: Any) -> None:
    shutdown = getattr(provider, "shutdown", None)
    if callable(shutdown):
        try:
            shutdown()
        except Exception:
            pass
