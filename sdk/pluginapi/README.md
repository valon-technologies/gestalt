# Plugin API

The proto definition at `v1/plugin.proto` is the gRPC contract between Gestalt
and its provider/runtime plugins. It defines three services:

- **ProviderPlugin** — implemented by provider plugins (metadata, operations,
  auth, session catalog).
- **RuntimePlugin** — implemented by runtime plugins (start/stop lifecycle).
- **RuntimeHost** — implemented by the Gestalt host for runtime callbacks
  (capability listing, cross-provider invocation).

## Regenerating stubs

The recommended tool is [buf](https://buf.build/docs/installation):

```sh
# From the repo root:
./scripts/generate-pluginapi.sh

# Or directly:
cd sdk/pluginapi && buf generate
```

This produces stubs for Go, TypeScript, and Python. Output locations:

| Language   | Path                          |
|------------|-------------------------------|
| Go         | `sdk/pluginapi/v1/`           |
| TypeScript | `sdk/plugin/typescript/gen/`  |
| Python     | `sdk/plugin/python/gen/`      |

If buf is not installed, the script falls back to `protoc` for Go-only
generation (the original workflow).

### Prerequisites

**buf (recommended):** `brew install bufbuild/buf/buf` or see
https://buf.build/docs/installation.

**protoc fallback (Go only):** requires `protoc`, `protoc-gen-go`, and
`protoc-gen-go-grpc` on `$PATH`.
