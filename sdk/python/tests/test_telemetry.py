import unittest
from unittest.mock import MagicMock, patch

from gestalt import telemetry


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


if __name__ == "__main__":
    unittest.main()
