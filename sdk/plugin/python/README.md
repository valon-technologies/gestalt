# gestalt-plugin

Python SDK for writing [Gestalt](https://github.com/valon-technologies/gestalt) provider and runtime plugins. It wraps the gRPC plugin contract with Pythonic helpers so you can focus on your integration logic rather than protobuf plumbing.

## Installation

```bash
pip install gestalt-plugin
```

Or install from source:

```bash
cd sdk/plugin/python
pip install -e .
```

## Quick start — provider plugin

```python
import json
from gestalt_plugin import (
    ExecuteRequest,
    OperationDef,
    OperationResult,
    serve_provider,
)

def execute(req: ExecuteRequest) -> OperationResult:
    return OperationResult(status=200, body=json.dumps(req.params))

serve_provider(
    name="my-provider",
    display_name="My Provider",
    operations=[
        OperationDef(name="hello", description="Say hello", method="POST"),
    ],
    execute=execute,
)
```

Register it in your gestalt config:

```yaml
providers:
  - name: my-provider
    type: subprocess
    command: ["python", "my_provider.py"]
```

## Quick start — runtime plugin

```python
from gestalt_plugin import serve_runtime, dial_runtime_host

def start(name: str, config: dict) -> None:
    host = dial_runtime_host()
    caps = host.list_capabilities()
    print(f"Runtime {name} started with {len(caps)} capabilities")

def stop() -> None:
    print("Runtime stopping")

serve_runtime(start=start, stop=stop)
```

## Types

| Type | Description |
|------|-------------|
| `OperationDef` | Declares a named operation with optional parameters |
| `ParameterDef` | Describes a single parameter (name, type, required, default) |
| `ExecuteRequest` | Passed to your `execute` callback with operation, params, and token |
| `OperationResult` | Return from `execute` with an HTTP-style status code and body |
| `TokenResponse` | Returned from OAuth callbacks |
| `Capability` | A provider+operation pair available to runtime plugins |

## OAuth support

Pass optional callbacks to `serve_provider` for OAuth flows:

```python
serve_provider(
    ...,
    auth_types=["oauth"],
    authorization_url=lambda state, scopes: "https://...",
    exchange_code=lambda code: TokenResponse(access_token="..."),
    refresh_token=lambda token: TokenResponse(access_token="..."),
)
```

## Protocol

This SDK implements the `gestalt.plugin.v1` gRPC contract. See the [protobuf definition](../../pluginapi/v1/) for the full protocol specification.
