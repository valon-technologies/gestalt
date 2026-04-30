"""OpenTelemetry helpers for provider-authored GenAI instrumentation.

The Gestalt runtime configures exporters from host-provided ``OTEL_*``
environment before a provider starts serving. Provider code should use this
module to create GenAI semantic-convention spans and metrics around the model,
agent, and tool work it owns.
"""

from ._telemetry import (
    GENAI_OPERATION_CHAT,
    GENAI_OPERATION_EXECUTE_TOOL,
    GENAI_OPERATION_INVOKE_AGENT,
    GENAI_PROVIDER_NAME,
    GENAI_TOOL_TYPE_DATASTORE,
    GENAI_TOOL_TYPE_EXTENSION,
    Operation,
    agent_invocation,
    configure_from_environment,
    model_operation,
    record_anthropic_usage,
    record_openai_usage,
    shutdown,
    tool_execution,
)

__all__ = [
    "GENAI_OPERATION_CHAT",
    "GENAI_OPERATION_EXECUTE_TOOL",
    "GENAI_OPERATION_INVOKE_AGENT",
    "GENAI_PROVIDER_NAME",
    "GENAI_TOOL_TYPE_DATASTORE",
    "GENAI_TOOL_TYPE_EXTENSION",
    "Operation",
    "agent_invocation",
    "configure_from_environment",
    "model_operation",
    "record_anthropic_usage",
    "record_openai_usage",
    "shutdown",
    "tool_execution",
]
