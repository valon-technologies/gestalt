from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ConversationRequest(_message.Message):
    __slots__ = ("conversation_id", "user_message", "user_id", "session_id", "model", "system_prompt", "allowed_tools", "settings")
    CONVERSATION_ID_FIELD_NUMBER: _ClassVar[int]
    USER_MESSAGE_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    SYSTEM_PROMPT_FIELD_NUMBER: _ClassVar[int]
    ALLOWED_TOOLS_FIELD_NUMBER: _ClassVar[int]
    SETTINGS_FIELD_NUMBER: _ClassVar[int]
    conversation_id: str
    user_message: str
    user_id: str
    session_id: str
    model: str
    system_prompt: str
    allowed_tools: _containers.RepeatedScalarFieldContainer[str]
    settings: AgentSettings
    def __init__(self, conversation_id: _Optional[str] = ..., user_message: _Optional[str] = ..., user_id: _Optional[str] = ..., session_id: _Optional[str] = ..., model: _Optional[str] = ..., system_prompt: _Optional[str] = ..., allowed_tools: _Optional[_Iterable[str]] = ..., settings: _Optional[_Union[AgentSettings, _Mapping]] = ...) -> None: ...

class AgentSettings(_message.Message):
    __slots__ = ("max_turns", "temperature", "max_tokens")
    MAX_TURNS_FIELD_NUMBER: _ClassVar[int]
    TEMPERATURE_FIELD_NUMBER: _ClassVar[int]
    MAX_TOKENS_FIELD_NUMBER: _ClassVar[int]
    max_turns: int
    temperature: float
    max_tokens: int
    def __init__(self, max_turns: _Optional[int] = ..., temperature: _Optional[float] = ..., max_tokens: _Optional[int] = ...) -> None: ...

class ConversationEvent(_message.Message):
    __slots__ = ("conversation_id", "text_delta", "thinking_delta", "tool_use", "tool_result", "turn_complete", "error")
    CONVERSATION_ID_FIELD_NUMBER: _ClassVar[int]
    TEXT_DELTA_FIELD_NUMBER: _ClassVar[int]
    THINKING_DELTA_FIELD_NUMBER: _ClassVar[int]
    TOOL_USE_FIELD_NUMBER: _ClassVar[int]
    TOOL_RESULT_FIELD_NUMBER: _ClassVar[int]
    TURN_COMPLETE_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    conversation_id: str
    text_delta: TextDelta
    thinking_delta: ThinkingDelta
    tool_use: ToolUse
    tool_result: ToolResult
    turn_complete: TurnComplete
    error: ErrorEvent
    def __init__(self, conversation_id: _Optional[str] = ..., text_delta: _Optional[_Union[TextDelta, _Mapping]] = ..., thinking_delta: _Optional[_Union[ThinkingDelta, _Mapping]] = ..., tool_use: _Optional[_Union[ToolUse, _Mapping]] = ..., tool_result: _Optional[_Union[ToolResult, _Mapping]] = ..., turn_complete: _Optional[_Union[TurnComplete, _Mapping]] = ..., error: _Optional[_Union[ErrorEvent, _Mapping]] = ...) -> None: ...

class TextDelta(_message.Message):
    __slots__ = ("content",)
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    content: str
    def __init__(self, content: _Optional[str] = ...) -> None: ...

class ThinkingDelta(_message.Message):
    __slots__ = ("content",)
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    content: str
    def __init__(self, content: _Optional[str] = ...) -> None: ...

class ToolUse(_message.Message):
    __slots__ = ("tool_call_id", "tool_name", "input_json")
    TOOL_CALL_ID_FIELD_NUMBER: _ClassVar[int]
    TOOL_NAME_FIELD_NUMBER: _ClassVar[int]
    INPUT_JSON_FIELD_NUMBER: _ClassVar[int]
    tool_call_id: str
    tool_name: str
    input_json: str
    def __init__(self, tool_call_id: _Optional[str] = ..., tool_name: _Optional[str] = ..., input_json: _Optional[str] = ...) -> None: ...

class ToolResult(_message.Message):
    __slots__ = ("tool_call_id", "content_json", "is_error")
    TOOL_CALL_ID_FIELD_NUMBER: _ClassVar[int]
    CONTENT_JSON_FIELD_NUMBER: _ClassVar[int]
    IS_ERROR_FIELD_NUMBER: _ClassVar[int]
    tool_call_id: str
    content_json: str
    is_error: bool
    def __init__(self, tool_call_id: _Optional[str] = ..., content_json: _Optional[str] = ..., is_error: bool = ...) -> None: ...

class TurnComplete(_message.Message):
    __slots__ = ("session_id", "model", "input_tokens", "output_tokens", "cost_usd", "duration_ms", "num_turns", "full_text")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    INPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    COST_USD_FIELD_NUMBER: _ClassVar[int]
    DURATION_MS_FIELD_NUMBER: _ClassVar[int]
    NUM_TURNS_FIELD_NUMBER: _ClassVar[int]
    FULL_TEXT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    model: str
    input_tokens: int
    output_tokens: int
    cost_usd: float
    duration_ms: int
    num_turns: int
    full_text: str
    def __init__(self, session_id: _Optional[str] = ..., model: _Optional[str] = ..., input_tokens: _Optional[int] = ..., output_tokens: _Optional[int] = ..., cost_usd: _Optional[float] = ..., duration_ms: _Optional[int] = ..., num_turns: _Optional[int] = ..., full_text: _Optional[str] = ...) -> None: ...

class ErrorEvent(_message.Message):
    __slots__ = ("message", "code", "recoverable")
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    CODE_FIELD_NUMBER: _ClassVar[int]
    RECOVERABLE_FIELD_NUMBER: _ClassVar[int]
    message: str
    code: str
    recoverable: bool
    def __init__(self, message: _Optional[str] = ..., code: _Optional[str] = ..., recoverable: bool = ...) -> None: ...

class ToolRequest(_message.Message):
    __slots__ = ("conversation_id", "user_id", "provider", "operation", "params_json")
    CONVERSATION_ID_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    OPERATION_FIELD_NUMBER: _ClassVar[int]
    PARAMS_JSON_FIELD_NUMBER: _ClassVar[int]
    conversation_id: str
    user_id: str
    provider: str
    operation: str
    params_json: str
    def __init__(self, conversation_id: _Optional[str] = ..., user_id: _Optional[str] = ..., provider: _Optional[str] = ..., operation: _Optional[str] = ..., params_json: _Optional[str] = ...) -> None: ...

class ToolResponse(_message.Message):
    __slots__ = ("result_json", "status", "error")
    RESULT_JSON_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    result_json: str
    status: int
    error: str
    def __init__(self, result_json: _Optional[str] = ..., status: _Optional[int] = ..., error: _Optional[str] = ...) -> None: ...

class ListToolsRequest(_message.Message):
    __slots__ = ("providers",)
    PROVIDERS_FIELD_NUMBER: _ClassVar[int]
    providers: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, providers: _Optional[_Iterable[str]] = ...) -> None: ...

class ListToolsResponse(_message.Message):
    __slots__ = ("tools",)
    TOOLS_FIELD_NUMBER: _ClassVar[int]
    tools: _containers.RepeatedCompositeFieldContainer[ToolDefinition]
    def __init__(self, tools: _Optional[_Iterable[_Union[ToolDefinition, _Mapping]]] = ...) -> None: ...

class ToolDefinition(_message.Message):
    __slots__ = ("name", "provider", "operation", "description", "input_schema_json", "read_only", "destructive")
    NAME_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    OPERATION_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    INPUT_SCHEMA_JSON_FIELD_NUMBER: _ClassVar[int]
    READ_ONLY_FIELD_NUMBER: _ClassVar[int]
    DESTRUCTIVE_FIELD_NUMBER: _ClassVar[int]
    name: str
    provider: str
    operation: str
    description: str
    input_schema_json: str
    read_only: bool
    destructive: bool
    def __init__(self, name: _Optional[str] = ..., provider: _Optional[str] = ..., operation: _Optional[str] = ..., description: _Optional[str] = ..., input_schema_json: _Optional[str] = ..., read_only: bool = ..., destructive: bool = ...) -> None: ...

class HealthRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class HealthResponse(_message.Message):
    __slots__ = ("ready", "status")
    READY_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    ready: bool
    status: str
    def __init__(self, ready: bool = ..., status: _Optional[str] = ...) -> None: ...

class ShutdownRequest(_message.Message):
    __slots__ = ("timeout_seconds",)
    TIMEOUT_SECONDS_FIELD_NUMBER: _ClassVar[int]
    timeout_seconds: int
    def __init__(self, timeout_seconds: _Optional[int] = ...) -> None: ...

class ShutdownResponse(_message.Message):
    __slots__ = ("clean",)
    CLEAN_FIELD_NUMBER: _ClassVar[int]
    clean: bool
    def __init__(self, clean: bool = ...) -> None: ...
