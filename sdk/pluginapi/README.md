# Plugin API

The proto definition at `v1/plugin.proto` is the gRPC contract between Gestalt
and its provider/runtime plugins. It defines three services:

- **ProviderPlugin** -- implemented by provider plugins (metadata, operations,
  auth, session catalog).
- **RuntimePlugin** -- implemented by runtime plugins (start/stop lifecycle).
- **RuntimeHost** -- implemented by the Gestalt host for runtime callbacks
  (capability listing, cross-provider invocation).

## Sub-module usage

This package is a standalone Go module so external plugins can depend on it
without pulling in the full Gestalt dependency tree:

```sh
go get github.com/valon-technologies/gestalt/sdk/pluginapi@latest
```

For most provider plugins, prefer depending on `sdk/pluginsdk` instead, which
wraps the raw proto types with a higher-level `Provider` interface and helpers
like `ServeProvider`.

For local development within the monorepo, the root `go.mod` uses a `replace`
directive:

```
replace github.com/valon-technologies/gestalt/sdk/pluginapi => ./sdk/pluginapi
```

## Tagging conventions

This module is tagged independently from the root module using the prefix
`sdk/pluginapi/`:

```
sdk/pluginapi/v0.1.0
sdk/pluginapi/v0.2.0
```

Go resolves module versions from these tags automatically when using
`go get github.com/valon-technologies/gestalt/sdk/pluginapi@vX.Y.Z`.

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
