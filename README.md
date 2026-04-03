# Gestalt

Gestalt is a gateway for integrations. It loads config, resolves remote specs and packaged plugins, and serves a single HTTP and MCP surface.

## Run

Local:

```sh
gestaltd
```

Production:

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
