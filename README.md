# Gestalt

Gestalt is a unified API gateway for integrations. It loads a YAML config, connects to REST, GraphQL, and MCP upstreams, manages OAuth and manual credentials, and serves a single API and MCP endpoint for all configured integrations.

## Local Quickstart

```sh
gestaltd
```

Bare `gestaltd` auto-generates a local config, boots with `auth.provider: none` and SQLite, and starts serving. No setup needed.

## Production Deployment

```sh
gestaltd init --config ./config.yaml
gestaltd serve --locked --config ./config.yaml
```

`init` resolves remote provider specs, installs plugin packages, and writes a lockfile. `serve --locked` starts the server from that prepared state without fetching or mutating anything.

## Commands

| Command | Purpose |
|---|---|
| `gestaltd` | Local dev: auto-prepares and serves. |
| `gestaltd init --config PATH` | Production prep: resolves providers and plugins and writes lock state. |
| `gestaltd serve --locked --config PATH` | Production runtime: serves from prepared state only. |
| `gestaltd validate --config PATH` | CI: validates config without starting the server. |
| `gestaltd plugin package --binary PATH --source SOURCE --output DIR` | Authoring: packages a plugin binary for distribution. |

## Docker

The published `valontechnologies/gestaltd` image:

- exposes port `8080`
- serves the API, embedded UI, `/health`, `/ready`, and `/mcp`
- defaults to `serve --locked --config /etc/gestalt/config.yaml`
- expects you to mount or bake a config file before startup
- includes a shell, `ca-certificates`, and `curl`

```dockerfile
FROM valontechnologies/gestaltd:latest AS init
USER root
COPY config.yaml /app/config.yaml
RUN ["/gestaltd", "init", "--config", "/app/config.yaml"]

FROM valontechnologies/gestaltd:latest
COPY --from=init /app/ /app/
CMD ["serve", "--locked", "--config", "/app/config.yaml"]
```

## Documentation

Full documentation is available at [docs.valon.tools](https://docs.valon.tools).

- [Getting Started](https://docs.valon.tools/getting-started)
- [Configuration](https://docs.valon.tools/concepts/configuration)
- [CLI Reference](https://docs.valon.tools/reference/cli)
- [Deployment](https://docs.valon.tools/deploy)
- [Write a Plugin](https://docs.valon.tools/tasks/write-a-plugin)
