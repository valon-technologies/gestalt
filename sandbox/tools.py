import json
import logging

import grpc

from sandbox.pb import sandbox_pb2, sandbox_pb2_grpc

log = logging.getLogger(__name__)


class ToolClient:
    def __init__(self, addr: str):
        self._channel = grpc.insecure_channel(addr)
        self._stub = sandbox_pb2_grpc.ToolServiceStub(self._channel)
        self._tools_cache: list[dict] | None = None
        self._tool_map: dict[str, tuple[str, str]] = {}

    def get_tools(self) -> list[dict]:
        if self._tools_cache is not None:
            return self._tools_cache

        resp = self._stub.ListTools(sandbox_pb2.ListToolsRequest())
        tools = []
        for td in resp.tools:
            name = f"{td.provider}_{td.operation}"
            self._tool_map[name] = (td.provider, td.operation)
            tool = {
                "name": name,
                "description": td.description,
                "input_schema": json.loads(td.input_schema_json) if td.input_schema_json else {"type": "object"},
            }
            tools.append(tool)

        self._tools_cache = tools
        return tools

    def execute(self, conversation_id: str, user_id: str, tool_name: str, params: dict) -> tuple[dict, bool]:
        provider, operation = self._tool_map.get(tool_name, ("", ""))
        if not provider:
            return {"error": f"unknown tool: {tool_name}"}, True

        try:
            resp = self._stub.ExecuteTool(sandbox_pb2.ToolRequest(
                conversation_id=conversation_id,
                user_id=user_id,
                provider=provider,
                operation=operation,
                params_json=json.dumps(params),
            ))
        except grpc.RpcError as e:
            log.error("tool execute failed: %s", e)
            return {"error": str(e)}, True

        if resp.is_error:
            return {"error": resp.error_message}, True
        result = json.loads(resp.result_json) if resp.result_json else {}
        return result, False

    def close(self):
        self._channel.close()
