# Gestalt

Gestalt is a unified API gateway for integrations. It loads a YAML config, connects to REST, GraphQL, and MCP upstreams, manages OAuth and manual credentials, and serves a single API and MCP endpoint for all configured integrations.

## Local Quickstart

```sh
gestaltd
```

Bare `gestaltd` auto-generates a local config, boots with `auth.provider: none` and SQLite, and starts serving. No setup needed.

## Production Deployment

```sh
gestaltd bundle --config ./config.yaml --output ./bundle
gestaltd serve --locked --config ./bundle/config.yaml
```

`bundle` resolves remote provider specs, installs plugin packages, writes a lockfile, and produces a self-contained output directory. `serve --locked` starts the server from that prepared state without fetching or mutating anything.

## Commands

| Command | Purpose |
|---|---|
| `gestaltd` | Local dev: auto-prepares and serves. |
| `gestaltd bundle --config PATH --output DIR` | Production prep: produces a self-contained deployable bundle. |
| `gestaltd serve --locked --config PATH` | Production runtime: serves from prepared state only. |
| `gestaltd validate --config PATH` | CI: validates config without starting the server. |
| `gestaltd plugin package --binary PATH --id ID --output FILE` | Authoring: packages a plugin binary for distribution. |

## Docker

The published `valontechnologies/gestaltd` image:

- exposes port `8080`
- serves the API, embedded UI, `/health`, `/ready`, and `/mcp`
- defaults to `serve --locked --config /etc/gestalt/config.yaml`
- expects you to mount or bake a config file before startup
- includes a shell, `ca-certificates`, and `curl`

```dockerfile
FROM valontechnologies/gestaltd:latest AS bundle
USER root
COPY config.yaml /src/config.yaml
RUN ["/gestaltd", "bundle", "--config", "/src/config.yaml", "--output", "/app"]

FROM valontechnologies/gestaltd:latest
COPY --from=bundle /app/ /app/
CMD ["serve", "--locked", "--config", "/app/config.yaml"]
```

## Documentation

Full documentation is available at [gestalt.run](https://gestalt.run).

- [Getting Started](/getting-started)
- [Configuration](/concepts/configuration)
- [CLI Reference](/reference/cli)
- [Deployment](/deploy)
- [Write a Plugin](/tasks/write-a-plugin)
