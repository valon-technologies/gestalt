"""ToolService gRPC client for calling Gestalt integration operations."""
import json
import logging

import grpc

from sandbox.pb import sandbox_pb2, sandbox_pb2_grpc

log = logging.getLogger("sandbox.tools")


class ToolClient:
    def __init__(self, tool_service_addr: str):
        self._channel = grpc.insecure_channel(tool_service_addr)
        self._stub = sandbox_pb2_grpc.ToolServiceStub(self._channel)
        self._tools_cache = None
        self._tool_map: dict[str, tuple[str, str]] = {}

    def get_tools(self) -> list[dict]:
        """Fetch tools from Go's ToolService, formatted for the Anthropic API."""
        if self._tools_cache is not None:
            return self._tools_cache

        response = self._stub.ListTools(sandbox_pb2.ListToolsRequest())
        tools = []
        for td in response.tools:
            tool_name = f"{td.provider}_{td.operation}"
            self._tool_map[tool_name] = (td.provider, td.operation)

            if td.input_schema_json:
                input_schema = json.loads(td.input_schema_json)
            else:
                input_schema = {"type": "object", "properties": {}}

            tools.append({
                "name": tool_name,
                "description": td.description or f"{td.provider} {td.operation}",
                "input_schema": input_schema,
            })

        self._tools_cache = tools
        log.info("loaded %d tools from ToolService", len(tools))
        return tools

    def execute(self, conversation_id: str, user_id: str, tool_name: str, params: dict) -> dict:
        """Execute a tool via Go's ToolService."""
        provider, operation = self._tool_map.get(tool_name, (None, None))
        if provider is None:
            return {"error": f"unknown tool: {tool_name}"}

        try:
            response = self._stub.ExecuteTool(sandbox_pb2.ToolRequest(
                conversation_id=conversation_id,
                user_id=user_id,
                provider=provider,
                operation=operation,
                params_json=json.dumps(params),
            ))

            if response.error:
                return {"error": response.error}

            return {"result": response.result_json, "status": response.status}
        except grpc.RpcError as e:
            log.error("tool execution failed: %s", e)
            details = e.details() if callable(getattr(e, "details", None)) else str(e)
            return {"error": f"tool execution failed: {details}"}

    def close(self):
        self._channel.close()
