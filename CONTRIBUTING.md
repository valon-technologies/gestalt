# Contributing

## Repository Layout

| Path | Purpose |
| --- | --- |
| `gestaltd/cmd/gestaltd` | Server entrypoint, command handling, and built-in registration. |
| `gestaltd/core` | Public interfaces for auth, datastore, secrets, and providers. |
| `gestaltd/internal` | Bootstrap, config loading, invocation, server routing, plugin process management, and other server internals. |
| `plugins` | External plugin source trees and release archives. |
| `sdk` | Public SDKs and plugin manifest definitions. |
| `gestaltd/ui` | Frontend that is embedded into the server build. |
| `docs` | Documentation site. |
| `gestaltd/deploy` | Docker and Helm deployment assets. |

## Working Principles

When you update docs, keep them aligned with:

- config structs in `gestaltd/internal/config`
- bootstrap wiring in `gestaltd/cmd/gestaltd` and `gestaltd/internal/bootstrap`
- HTTP routes in `gestaltd/internal/server`
- deployment assets in `gestaltd/Dockerfile` and `gestaltd/deploy/helm`

The easiest way to make docs drift is to copy previous prose instead of reading the code path that actually implements the feature.

## Useful Commands

```sh
cd gestaltd
go test ./...

cd ../gestalt
cargo test

cd ../gestaltd/ui
npm ci
npm run typecheck
npm run build

cd ../../docs
npm ci
npm run typecheck
npm run build
```

## Release Tags

Release workflows use scoped tags:

- CLI: `gestalt/v<version>`
- Server: `gestaltd/v<version>`
- Go SDK: `sdk/go/v<version>`
- Plugins: `plugin/<plugin>/v<version>`

Keep the bare semantic version aligned across artifacts when they are meant to ship together.

## Adding New Built-Ins

If you add a new built-in auth provider, datastore, secret manager, or named provider:

1. Register it in the relevant `gestaltd/cmd/gestaltd` file.
2. Add or update tests.
3. Update [Built-in Providers](https://docs.valon.tools/reference/built-in-providers).
4. Update any docs or examples that describe the supported inventory.
