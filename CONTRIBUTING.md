# Contributing

Thanks for contributing. Before opening a PR, run the checks that match the parts of the repo you changed and keep the docs aligned with the code paths that actually implement the behavior.

## Repository Layout

| Path | Purpose |
| --- | --- |
| `gestaltd/cmd/gestaltd` | Server entrypoint, command handling, and built-in registration. |
| `gestaltd/core` | Public Go interfaces for auth, datastore, secrets, cache, telemetry, and providers. |
| `gestaltd/internal` | Bootstrap, config loading, invocation, HTTP and MCP serving, operator lifecycle, and other server internals. |
| `gestaltd/internal/webui` | Embedded admin UI asset serving and related tests. |
| `gestaltd/deploy` | Docker and Helm deployment assets for the server. |
| `gestalt` | Rust CLI client. |
| `sdk/go` | Go SDK. |
| `sdk/python` | Python SDK. |
| `sdk/rust` | Rust SDK. |
| `sdk/typescript` | TypeScript SDK. |
| `plugins` | Checked-in first-party plugin release artifacts. |
| `docs` | Documentation site source for `https://gestaltd.ai`. |

## Working Principles

When you update docs, keep them aligned with:

- config structs in `gestaltd/internal/config`
- bootstrap wiring in `gestaltd/cmd/gestaltd` and `gestaltd/internal/bootstrap`
- HTTP and MCP behavior in `gestaltd/internal/server` and `gestaltd/internal/mcp`
- admin and mounted UI behavior in `gestaltd/internal/webui` and `gestaltd/internal/server/mounted_ui.go`
- deployment assets in `gestaltd/Dockerfile` and `gestaltd/deploy/helm`

The easiest way to make docs drift is to copy previous prose instead of reading the code path that actually implements the feature.

## Tooling

Install only what you need for the parts of the repo you are changing:

- Go for `gestaltd` and `sdk/go`
- Rust for `gestalt` and `sdk/rust`
- Node.js and npm for `docs`
- Bun for `sdk/typescript`
- `uv` for `sdk/python`

If you want the pre-push hook, enable it once:

```sh
git config core.hooksPath .githooks
```

The hook runs the same style and validation checks as CI for the areas changed on your branch. Bypass it with `git push --no-verify` when necessary.

## Useful Commands

Run the checks that match the code you touched.

### Server

```sh
cd gestaltd
go test ./...
```

### CLI

```sh
cd gestalt
cargo test --workspace
```

### Go SDK

```sh
cd sdk/go
go test ./...
```

### Python SDK

```sh
cd sdk/python
uv sync --frozen --group dev
uv run ruff check .
uv run ty check --exclude 'gestalt/gen/**' gestalt scripts tests
uv run vulture --config pyproject.toml
uv run python -m unittest discover -s tests
```

### Rust SDK

```sh
cd sdk/rust
cargo fmt --check
cargo test
cargo clippy --all-targets -- -D warnings
```

### TypeScript SDK

```sh
cd sdk/typescript
bun install --frozen-lockfile
bun run check
```

### Docs

```sh
cd docs
npm ci
npm run typecheck
npm run build
```

## Release Tags

Release workflows use scoped tags:

- CLI: `gestalt/v<version>`
- Server: `gestaltd/v<version>`
- Go SDK: `sdk/go/v<version>`
- Python SDK: `sdk/python/v<version>`
- Rust SDK: `sdk/rust/v<version>`
- TypeScript SDK: `sdk/typescript/v<version>`
- Plugins: `plugin/<plugin>/v<version>`

Keep the bare semantic version aligned across artifacts when they are meant to ship together.

## Adding New Built-Ins

If you add a new built-in auth provider, datastore, secret manager, telemetry sink, audit sink, or named provider:

1. Register it in the relevant `gestaltd/cmd/gestaltd` file.
2. Add or update tests.
3. Update [First-Party Providers](https://gestaltd.ai/reference/built-in-providers).
4. Update any docs or examples that describe the supported inventory.
