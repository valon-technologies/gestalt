# Plugin API

The proto definition at `v1/plugin.proto` is the gRPC contract between Gestalt
and its provider/runtime plugins. It defines three services:

- **ProviderPlugin** -- implemented by provider plugins (metadata, operations,
  auth, session catalog).
- **RuntimePlugin** -- implemented by runtime plugins (start/stop lifecycle).
- **RuntimeHost** -- implemented by the Gestalt host for runtime callbacks
  (capability listing, cross-provider invocation).

## Using the SDK as a dependency

External plugins should depend on the `pluginsdk` module, which re-exports
everything needed to serve a provider or runtime plugin:

```sh
go get github.com/valon-technologies/gestalt/sdk/pluginsdk@latest
```

The `pluginsdk` module depends on `pluginapi` transitively, so you do not need
to add `pluginapi` to your `go.mod` explicitly unless you reference the
generated proto types directly:

```sh
go get github.com/valon-technologies/gestalt/sdk/pluginapi@latest
```

Working examples live in `examples/plugins/provider-go/` and
`examples/plugins/runtime-go/`.

## Sub-module versioning

Both `sdk/pluginapi` and `sdk/pluginsdk` are independent Go modules with their
own `go.mod` files. They are tagged separately from the root module using the
Go sub-module convention:

```
sdk/pluginapi/v0.1.0
sdk/pluginsdk/v0.1.0
```

When cutting a release, use the automated script:

```sh
./scripts/release-sdk.sh 0.1.0        # release both modules
./scripts/release-sdk.sh 0.1.0 --dry-run  # preview without executing
```

The script tags `pluginapi` first, creates a temporary git worktree to pin
`pluginsdk` to the released `pluginapi` version, tags `pluginsdk`, pushes
both tags, and cleans up. Main branch is never modified.

The root module and example modules use `replace` directives to point at the
local source tree, so they always build against HEAD regardless of published
tags.

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
