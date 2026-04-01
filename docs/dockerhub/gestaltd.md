# gestaltd Docker image

`gestaltd` is a self-hosted integration gateway. You describe your platform in one YAML file, and `gestaltd` turns that file into a running server with:

- an HTTP API
- an embedded web UI
- `/health` and `/ready` endpoints
- an `/mcp` endpoint when integrations expose tools
- support for REST, GraphQL, MCP, and packaged plugins

## Quick reference

- Image: `valontechnologies/gestaltd`
- Default port: `8080`
- Default command:

  ```sh
  /gestaltd serve --locked --config /etc/gestalt/config.yaml
  ```

- Default config path: `/etc/gestalt/config.yaml`
- This image is not zero-config. Mount or bake a config file before starting it.
- Locked startup is the default. If your config uses `plugin.package`,
  `plugin.source`, or a packaged UI, run `init` first.

## Supported tags

The publish workflows maintain these tag shapes:

- `latest`
- `<version>`
- `<sha>`

The image is published for `linux/amd64` and `linux/arm64`.

## What the image includes

The image is Alpine-based and includes `gestaltd`, a shell, and `ca-certificates`. It runs as `nobody:nobody` by default.

## Run a simple static config

If your config does not need prepared artifacts, mount it directly:

```sh
docker run --rm \
  -p 8080:8080 \
  -v "$(pwd)/gestalt.yaml:/etc/gestalt/config.yaml:ro" \
  -v gestalt-data:/data \
  valontechnologies/gestaltd:latest
```

Example minimal config:

```yaml
server:
  port: 8080
  encryption_key: ${GESTALT_ENCRYPTION_KEY}

auth:
  provider: none

datastore:
  provider: sqlite
  config:
    path: /data/gestalt.db

integrations: {}
```

## Run a prepared production image

If your config uses `plugin.package`, `plugin.source`, or a packaged UI,
prepare it during the image build:

```dockerfile
FROM valontechnologies/gestaltd:latest AS init
USER root
COPY config.yaml /app/config.yaml
RUN ["/gestaltd", "init", "--config", "/app/config.yaml"]

FROM valontechnologies/gestaltd:latest
COPY --from=init /app/ /app/
CMD ["serve", "--locked", "--config", "/app/config.yaml"]
```

This is the recommended production pattern.

## Compose example

```yaml
services:
  gestaltd:
    image: valontechnologies/gestaltd:latest
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/etc/gestalt/config.yaml:ro
      - gestalt-data:/data
    environment:
      GESTALT_ENCRYPTION_KEY: change-me

volumes:
  gestalt-data:
```

## Configuration and environment variables

Gestalt expands `${VAR}` placeholders before YAML decode. The image also supports the common `*_FILE` convention for those placeholders:

- if `VAR` is set, `${VAR}` resolves to that value
- otherwise, if `VAR_FILE` is set, `${VAR}` resolves to the contents of that file

That means this works well with Docker secrets:

```yaml
server:
  encryption_key: ${GESTALT_ENCRYPTION_KEY}
```

```sh
docker run --rm \
  -p 8080:8080 \
  -v "$(pwd)/gestalt.yaml:/etc/gestalt/config.yaml:ro" \
  -v /run/secrets/gestalt-encryption-key:/run/secrets/gestalt-encryption-key:ro \
  -e GESTALT_ENCRYPTION_KEY_FILE=/run/secrets/gestalt-encryption-key \
  valontechnologies/gestaltd:latest
```

For more advanced setups, Gestalt also supports `secret://...` references with `env`, `file`, `google_secret_manager`, `aws_secrets_manager`, `vault`, and `azure_key_vault` secret providers.

## Health endpoints

The container exposes:

- `GET /health` for liveness
- `GET /ready` for readiness

The embedded web UI is served from `/` on the same port.

## SQLite and `/data`

SQLite works well for:

- local development
- demos
- single-instance deployments with persistent storage

If you choose SQLite, store the database on mounted durable storage such as `/data/gestalt.db`.

For horizontally scaled deployments, prefer Postgres or MySQL.

## Debugging

The image includes a shell, so you can exec into a running container or start an interactive session:

```sh
docker run --rm -it --entrypoint sh valontechnologies/gestaltd:latest
```

You can also use it to check startup behavior directly:

```sh
docker run --rm valontechnologies/gestaltd:latest --help
```

## Packaging plugins

If you build a plugin package in Docker, compile your binaries into a plugin
directory first, then package that directory with `gestaltd`:

```dockerfile
FROM valontechnologies/gestaltd:latest AS gestaltd

FROM golang:1.26-alpine AS build
RUN apk add --no-cache git
COPY --from=gestaltd /gestaltd /usr/local/bin/gestaltd
WORKDIR /src
COPY . .
RUN go build -o ./my-plugin/artifacts/linux/amd64/provider ./plugins/cmd/myplugin && \
    gestaltd plugin package --input ./my-plugin --output ./dist/my-plugin.tar.gz && \
    gestaltd init --config ./deploy/config.yaml
```

## Caveats

- The published image defaults to locked startup. A missing config file or missing prepared state causes startup to fail fast.
- `docker run valontechnologies/gestaltd:latest` by itself is expected to fail because the image does not auto-generate config in-container.
- The image includes a shell for debugging.
- If you use SQLite, do not scale to multiple replicas.

## Learn more

- Docs: https://docs.toolshed.peachstreet.dev
- CLI reference: https://docs.toolshed.peachstreet.dev/reference/cli
- Deployment docs: https://docs.toolshed.peachstreet.dev/deploy
- Source: https://github.com/valon-technologies/gestalt
