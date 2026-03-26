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

When cutting a release:

1. Tag the pluginapi module first since pluginsdk depends on it:
   ```sh
   git tag sdk/pluginapi/v0.X.Y
   git push origin sdk/pluginapi/v0.X.Y
   ```
2. Update the `pluginsdk/go.mod` require for pluginapi to the new tag, then tag:
   ```sh
   git tag sdk/pluginsdk/v0.X.Y
   git push origin sdk/pluginsdk/v0.X.Y
   ```
3. Run `GOPROXY=proxy.golang.org go list -m` for both modules to warm the proxy
   cache.

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
