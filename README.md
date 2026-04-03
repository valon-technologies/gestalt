# Gestalt

Gestalt is organized as a small monorepo with three primary codebases:

| Path | What it contains |
| --- | --- |
| `gestaltd` | The Go server daemon. It loads config, resolves remote specs and packaged plugins, serves the HTTP API and MCP surface, embeds the UI, and includes Docker and Helm deployment assets. |
| `gestalt` | The Rust CLI client. It connects to a running `gestaltd` instance for authentication, operations, and local operator workflows. |
| `gestaltd/ui` | The Next.js web UI that ships with `gestaltd` and is embedded into server builds. |

Supporting directories:

| Path | What it contains |
| --- | --- |
| `plugins` | Built-in and example plugin packages. |
| `sdk` | Shared SDKs and plugin manifest definitions. |
| `docs` | The documentation site. |

## Run

Server from source:

```sh
cd gestaltd
go run ./cmd/gestaltd
```

CLI:

```sh
cd gestalt
cargo run -- --help
```

UI:

```sh
cd gestaltd/ui
npm ci
npm run dev
```

Installed production server:

```sh
gestaltd init --config ./config.yaml
gestaltd serve --locked --config ./config.yaml
```

`init` resolves remote state and writes a lockfile. `serve --locked` starts from that prepared state without fetching or mutating anything.

## Commands

- `gestaltd`
- `gestaltd init --config PATH`
- `gestaltd serve --locked --config PATH`
- `gestaltd validate --config PATH`
- `gestaltd plugin package --input PATH --output PATH`
- `gestaltd plugin release --version VERSION [--output DIR] [--platform PLATFORMS]`

## Plugins

Versioned provider plugins are published from [`valon-technologies/gestalt-plugins`](https://github.com/valon-technologies/gestalt-plugins).

Managed installs use:

```yaml
plugin:
  source: github.com/valon-technologies/gestalt-plugins/<plugin>
  version: 0.0.1-alpha.1
```

Use bare SemVer in config. The corresponding GitHub release tag format is `<plugin>/v<version>`.

## Documentation

Docs: [docs.valon.tools](https://docs.valon.tools)

- [Getting Started](https://docs.valon.tools/getting-started)
- [Configuration](https://docs.valon.tools/concepts/configuration)
- [CLI Reference](https://docs.valon.tools/reference/cli)
- [Deployment](https://docs.valon.tools/deploy)
- [Write a Plugin](https://docs.valon.tools/tasks/write-a-plugin)
