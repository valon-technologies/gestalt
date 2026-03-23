import json
import logging
import time

import anthropic

from sandbox.pb import sandbox_pb2, sandbox_pb2_grpc
from sandbox.tools import ToolClient

log = logging.getLogger(__name__)

DEFAULT_MAX_TURNS = 25
DEFAULT_MAX_TOKENS = 8192
DEFAULT_MODEL = "claude-sonnet-4-20250514"


class SandboxServicer(sandbox_pb2_grpc.SandboxServiceServicer):
    def __init__(self, tool_client: ToolClient):
        self._tool_client = tool_client
        self._anthropic = anthropic.Anthropic()
        self._shutting_down = False

    def Health(self, request, context):
        return sandbox_pb2.HealthResponse(
            ready=not self._shutting_down,
            status="shutting_down" if self._shutting_down else "ok",
        )

    def Shutdown(self, request, context):
        self._shutting_down = True
        return sandbox_pb2.ShutdownResponse(clean=True)

    def Converse(self, request, context):
        conversation_id = request.conversation_id
        model = request.model or DEFAULT_MODEL
        system_prompt = request.system_prompt or ""
        max_turns = request.settings.max_turns if request.settings.max_turns else DEFAULT_MAX_TURNS
        max_tokens = request.settings.max_tokens if request.settings.max_tokens else DEFAULT_MAX_TOKENS
        temperature = request.settings.temperature if request.settings.temperature else None

        messages = [{"role": "user", "content": request.user_message}]

        try:
            tools = self._tool_client.get_tools()
        except Exception as e:
            log.error("failed to get tools: %s", e)
            yield sandbox_pb2.ConversationEvent(
                conversation_id=conversation_id,
                error=sandbox_pb2.ErrorEvent(message=f"failed to get tools: {e}", code="tool_error"),
            )
            return

        input_tokens = 0
        output_tokens = 0
        text_parts: list[str] = []
        num_turns = 0
        start_time = time.monotonic()

        try:
            for _ in range(max_turns):
                if not context.is_active():
                    return

                num_turns += 1
                kwargs = {
                    "model": model,
                    "max_tokens": max_tokens,
                    "messages": messages,
                }
                if system_prompt:
                    kwargs["system"] = system_prompt
                if tools:
                    kwargs["tools"] = tools
                if temperature is not None:
                    kwargs["temperature"] = temperature

                with self._anthropic.messages.stream(**kwargs) as stream:
                    for event in stream:
                        if not context.is_active():
                            return

                        if event.type == "content_block_delta":
                            if event.delta.type == "text_delta":
                                text_parts.append(event.delta.text)
                                yield sandbox_pb2.ConversationEvent(
                                    conversation_id=conversation_id,
                                    text_delta=sandbox_pb2.TextDelta(content=event.delta.text),
                                )
                            elif event.delta.type == "thinking_delta":
                                yield sandbox_pb2.ConversationEvent(
                                    conversation_id=conversation_id,
                                    thinking_delta=sandbox_pb2.ThinkingDelta(content=event.delta.thinking),
                                )

                    final_message = stream.get_final_message()

                input_tokens += final_message.usage.input_tokens
                output_tokens += final_message.usage.output_tokens

                tool_use_blocks = [b for b in final_message.content if b.type == "tool_use"]
                if not tool_use_blocks:
                    break

                messages.append({"role": "assistant", "content": final_message.content})
                tool_results = []

                for block in tool_use_blocks:
                    if not context.is_active():
                        return

                    yield sandbox_pb2.ConversationEvent(
                        conversation_id=conversation_id,
                        tool_use=sandbox_pb2.ToolUse(
                            tool_call_id=block.id,
                            tool_name=block.name,
                            input_json=json.dumps(block.input),
                        ),
                    )

                    result, is_error = self._tool_client.execute(
                        conversation_id=conversation_id,
                        user_id=request.user_id,
                        tool_name=block.name,
                        params=block.input,
                    )

                    yield sandbox_pb2.ConversationEvent(
                        conversation_id=conversation_id,
                        tool_result=sandbox_pb2.ToolResult(
                            tool_call_id=block.id,
                            content_json=json.dumps(result),
                            is_error=is_error,
                        ),
                    )

                    tool_results.append({
                        "type": "tool_result",
                        "tool_use_id": block.id,
                        "content": json.dumps(result),
                        "is_error": is_error,
                    })

                messages.append({"role": "user", "content": tool_results})

        except Exception as e:
            log.error("converse error: %s", e)
            yield sandbox_pb2.ConversationEvent(
                conversation_id=conversation_id,
                error=sandbox_pb2.ErrorEvent(message=str(e), code="internal_error"),
            )
            return

        duration_ms = int((time.monotonic() - start_time) * 1000)
        yield sandbox_pb2.ConversationEvent(
            conversation_id=conversation_id,
            turn_complete=sandbox_pb2.TurnComplete(
                session_id=request.session_id,
                model=model,
                input_tokens=input_tokens,
                output_tokens=output_tokens,
                duration_ms=duration_ms,
                num_turns=num_turns,
                full_text="".join(text_parts),
            ),
        )
