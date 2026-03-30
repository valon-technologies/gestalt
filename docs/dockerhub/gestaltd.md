# gestaltd Docker image

`gestaltd` is a self-hosted integration runtime. You describe your platform in one YAML file, and `gestaltd` turns that file into a running server with:

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
- Locked startup is the default. If your config depends on remote OpenAPI or GraphQL specs, or on `plugin.package`, bundle the config first and run from the prepared output.

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

## Run a bundled production image

If your config references remote OpenAPI or GraphQL sources, or `plugin.package`, prepare it during the image build:

```dockerfile
FROM valontechnologies/gestaltd:latest AS bundle
USER root
COPY config.yaml /src/config.yaml
RUN ["/gestaltd", "bundle", "--config", "/src/config.yaml", "--output", "/app"]

FROM valontechnologies/gestaltd:latest
COPY --from=bundle /app/ /app/
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

For more advanced setups, Gestalt also supports `secret://...` references with `env`, `file`, and `gcp_secret_manager` secret providers.

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

## Building plugins and bundles

To compile Go plugins, use a standard `golang:` image and copy `gestaltd` from the runtime image:

```dockerfile
FROM valontechnologies/gestaltd:latest AS gestaltd

FROM golang:1.26-alpine AS build
RUN apk add --no-cache git
COPY --from=gestaltd /gestaltd /usr/local/bin/gestaltd
WORKDIR /src
COPY . .
RUN go build -o /tmp/myplugin ./plugins/cmd/myplugin && \
    gestaltd plugin package --binary /tmp/myplugin --id example/myplugin --output ./deploy/plugins/myplugin.tar.gz && \
    gestaltd bundle --config ./deploy/config.yaml --output /app
```

## Caveats

- The published image defaults to locked startup. A missing config file or missing prepared state causes startup to fail fast.
- `docker run valontechnologies/gestaltd:latest` by itself is expected to fail because the image does not auto-generate config in-container.
- The image includes a shell for debugging.
- If you use SQLite, do not scale to multiple replicas.

## Learn more

- Docs: https://gestalt.run
- CLI reference: https://gestalt.run/reference/cli
- Deployment docs: https://gestalt.run/deploy
- Source: https://github.com/valon-technologies/gestalt
