"""Minimal echo provider plugin matching the Go echo plugin."""

import json

from gestalt_plugin import (
    ExecuteRequest,
    OperationDef,
    OperationResult,
    serve_provider,
)


def execute(req: ExecuteRequest) -> OperationResult:
    if req.operation != "echo":
        return OperationResult(status=400, body=f"unknown operation: {req.operation}")
    return OperationResult(status=200, body=json.dumps(req.params))


if __name__ == "__main__":
    serve_provider(
        name="echo",
        display_name="Echo",
        description="Echoes back the input parameters",
        operations=[
            OperationDef(
                name="echo",
                description="Echo back input params as JSON",
                method="POST",
            ),
        ],
        execute=execute,
    )
