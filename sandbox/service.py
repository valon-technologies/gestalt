"""SandboxService gRPC implementation."""
import json
import logging
import time
from typing import Iterator

import anthropic
import grpc

from sandbox.pb import sandbox_pb2, sandbox_pb2_grpc
from sandbox.tools import ToolClient

log = logging.getLogger("sandbox.service")


class SandboxServicer(sandbox_pb2_grpc.SandboxServiceServicer):
    def __init__(self, tool_client: ToolClient):
        self._tool_client = tool_client
        self._status = "ready"
        self._anthropic = anthropic.Anthropic()

    def Health(self, request, context):
        return sandbox_pb2.HealthResponse(
            ready=self._status == "ready",
            status=self._status,
        )

    def Shutdown(self, request, context):
        self._status = "shutting_down"
        return sandbox_pb2.ShutdownResponse(clean=True)

    def Converse(self, request: sandbox_pb2.ConversationRequest, context) -> Iterator[sandbox_pb2.ConversationEvent]:
        """Run a conversation turn with Claude, streaming events back to Go."""
        self._status = "busy"
        conv_id = request.conversation_id
        start_time = time.monotonic()
        full_text = ""
        input_tokens = 0
        output_tokens = 0

        try:
            messages = self._build_messages(request)
            tools = self._tool_client.get_tools()

            model = request.model or "claude-sonnet-4-20250514"
            max_tokens = 8192
            if request.settings.max_tokens > 0:
                max_tokens = request.settings.max_tokens
            temperature = None
            if request.settings.temperature > 0:
                temperature = request.settings.temperature

            max_turns = 10
            if request.settings.max_turns > 0:
                max_turns = request.settings.max_turns

            for turn in range(max_turns):
                if not context.is_active():
                    break

                kwargs = dict(
                    model=model,
                    max_tokens=max_tokens,
                    messages=messages,
                    stream=True,
                )
                if request.system_prompt:
                    kwargs["system"] = request.system_prompt
                if tools:
                    kwargs["tools"] = tools
                if temperature is not None:
                    kwargs["temperature"] = temperature

                tool_calls = []
                current_text = ""

                with self._anthropic.messages.stream(**kwargs) as stream:
                    for event in stream:
                        if not context.is_active():
                            break

                        if hasattr(event, "type") and event.type == "content_block_delta":
                            if hasattr(event.delta, "text"):
                                current_text += event.delta.text
                                full_text += event.delta.text
                                yield sandbox_pb2.ConversationEvent(
                                    conversation_id=conv_id,
                                    text_delta=sandbox_pb2.TextDelta(content=event.delta.text),
                                )

                    final_message = stream.get_final_message()
                    input_tokens += final_message.usage.input_tokens
                    output_tokens += final_message.usage.output_tokens

                    tool_calls = [
                        {"id": block.id, "name": block.name, "input": block.input}
                        for block in final_message.content
                        if block.type == "tool_use"
                    ]

                if not tool_calls:
                    break

                # Execute each tool call and feed results back for the next turn
                messages.append({"role": "assistant", "content": final_message.content})
                tool_results = []

                for tc in tool_calls:
                    yield sandbox_pb2.ConversationEvent(
                        conversation_id=conv_id,
                        tool_use=sandbox_pb2.ToolUse(
                            tool_call_id=tc["id"],
                            tool_name=tc["name"],
                            input_json=json.dumps(tc["input"]),
                        ),
                    )

                    result = self._tool_client.execute(
                        conversation_id=conv_id,
                        user_id=request.user_id,
                        tool_name=tc["name"],
                        params=tc["input"],
                    )

                    is_error = result.get("error", "") != ""
                    content = result.get("error") if is_error else result.get("result", "")

                    yield sandbox_pb2.ConversationEvent(
                        conversation_id=conv_id,
                        tool_result=sandbox_pb2.ToolResult(
                            tool_call_id=tc["id"],
                            content_json=json.dumps(content) if not isinstance(content, str) else content,
                            is_error=is_error,
                        ),
                    )

                    tool_results.append({
                        "type": "tool_result",
                        "tool_use_id": tc["id"],
                        "content": str(content),
                        "is_error": is_error,
                    })

                messages.append({"role": "user", "content": tool_results})

            duration_ms = int((time.monotonic() - start_time) * 1000)
            yield sandbox_pb2.ConversationEvent(
                conversation_id=conv_id,
                turn_complete=sandbox_pb2.TurnComplete(
                    model=model,
                    input_tokens=input_tokens,
                    output_tokens=output_tokens,
                    duration_ms=duration_ms,
                    num_turns=turn + 1,
                    full_text=full_text,
                ),
            )

        except Exception as e:
            log.exception("error in conversation %s", conv_id)
            yield sandbox_pb2.ConversationEvent(
                conversation_id=conv_id,
                error=sandbox_pb2.ErrorEvent(
                    message=str(e),
                    code="internal",
                    recoverable=False,
                ),
            )
        finally:
            self._status = "ready"

    def _build_messages(self, request):
        messages = []
        if request.user_message:
            messages.append({"role": "user", "content": request.user_message})
        return messages
