# gestaltd Docker image

`gestaltd` is a platform for self-hostable, configurable integrations and tooling. You describe your platform in one YAML file, and `gestaltd` turns that file into a running server with:

- an HTTP API
- a public client UI at `/` when enabled
- a built-in admin UI at `/admin`
- `/health` and `/ready` endpoints
- an `/mcp` endpoint when providers expose tools
- support for REST, GraphQL, MCP, and source or published plugins

> **Alpha.** Gestalt is under active development. Images are tagged
> with alpha versions and may introduce breaking changes. See the
> [documentation](https://gestaltd.ai) for the latest guidance.

## Quick reference

- Image: `valontechnologies/gestaltd`
- Default port: `8080`
- Default command:

  ```sh
  /gestaltd serve --locked --config /etc/gestalt/config.yaml --artifacts-dir /data
  ```

- Default config path: `/etc/gestalt/config.yaml`
- Default writable data and artifacts dir: `/data`
- This image is not zero-config. Mount or bake a config file before starting it.
- Locked startup is the default. If your config uses
  `plugins.*.provider.source.ref`, `auth.provider.source.ref`,
  `datastores.*.provider.source.ref`, or `ui.provider.source.ref`, run `init`
  first.

## Supported tags

Docker Hub publishes these tag shapes for each release:

| Tag | Base | Shell | Size |
| --- | --- | --- | --- |
| `latest`, `<version>` | `scratch` (static) | No | Smallest |
| `latest-alpine`, `<version>-alpine` | Alpine 3.23 | Yes | Small |
| `latest-debian`, `<version>-debian` | Debian bookworm-slim | Yes | Medium |

All tags are published for `linux/amd64`, `linux/arm64`, and `linux/arm/v7`.

## What the image includes

The default image is a static build: just the `gestaltd` binary and CA
certificates on a `scratch` base. There is no shell, no package manager, and
minimal attack surface.

For debugging access or plugin compatibility, use the `-alpine` or `-debian`
variants. These include a shell, `ca-certificates`, and a writable `/data`
directory owned by `nobody`.

## Run a simple config

Mount a config file and writable `/data` volume before starting the container:

```sh
docker run --rm \
  -p 8080:8080 \
  -e GESTALT_ENCRYPTION_KEY=change-me \
  -v "$(pwd)/gestalt.yaml:/etc/gestalt/config.yaml:ro" \
  -v gestalt-data:/data \
  valontechnologies/gestaltd:latest
```

The default image command still points `--artifacts-dir` at `/data`, so keeping
that volume mounted is the safe default even when your current config does not
need prepared plugins.

Example minimal config:

```yaml
server:
  public:
    port: 8080
  encryptionKey: ${GESTALT_ENCRYPTION_KEY}

datastores:
  main:
    provider:
      source:
        ref: github.com/valon-technologies/gestalt-providers/datastore/relationaldb
        version: 0.0.1-alpha.1
    config:
      dsn: sqlite:///data/gestalt.db
datastore: main

plugins: {}
ui:
  provider: none
```

Example with a separate internal-only management listener:

```yaml
server:
  public:
    host: 0.0.0.0
    port: 8080
  management:
    host: 0.0.0.0
    port: 9090
  encryptionKey: ${GESTALT_ENCRYPTION_KEY}

datastores:
  main:
    provider:
      source:
        ref: github.com/valon-technologies/gestalt-providers/datastore/relationaldb
        version: 0.0.1-alpha.1
    config:
      dsn: sqlite:///data/gestalt.db
datastore: main

plugins: {}
ui:
  provider: none
```

```sh
docker run --rm \
  -p 8080:8080 \
  -p 127.0.0.1:9090:9090 \
  -e GESTALT_ENCRYPTION_KEY=change-me \
  -v "$(pwd)/gestalt.yaml:/etc/gestalt/config.yaml:ro" \
  -v gestalt-data:/data \
  valontechnologies/gestaltd:latest
```

This keeps `/admin` and `/metrics` off the public interface while still making
them reachable from the host at `127.0.0.1:9090`.

For production-style deployments, prefer this split-listener pattern over
leaving `/admin` and `/metrics` on the public listener. If you also set
`server.baseUrl`, the management admin UI can link back to the public client
UI hostname; otherwise it omits that link.

## Run a locked production image

For production deployments, use `gestaltd serve --locked` and ship
`gestalt.lock.json` with the image. There are two valid ways to do that:

- Vendored artifacts: run `gestaltd init` before `docker build` and copy both
  `gestalt.lock.json` and `.gestaltd/` into the image.
- Lockfile-only: ship `gestalt.lock.json` but exclude `.gestaltd/` from the
  repo and build context. `gestaltd` recreates `.gestaltd/` from the lockfile
  at startup.

Vendored artifacts are more hermetic and usually give faster cold starts.
Lockfile-only images are smaller and avoid generated-file churn, but require
runtime network access, source auth, verified hashes in the lockfile, and a
writable artifacts directory.

### Vendored artifacts

For deterministic production images, run `gestaltd init` before `docker build`
and copy the prepared state into the image.

For a config at `deploy/config.yaml`, `init` writes:

```text
deploy/
  config.yaml
  gestalt.lock.json
  .gestaltd/providers/...
  .gestaltd/ui/...
```

Build the image from that prepared directory:

```dockerfile
FROM valontechnologies/gestaltd:latest-alpine
COPY deploy/ /app/
CMD ["serve", "--locked", "--config", "/app/config.yaml"]
```

Use the `-alpine` variant as the base for derived images since it includes a
shell for build steps.

If you intentionally want the image build itself to generate prepared state,
`RUN gestaltd init` in a build stage also works, but it is a build-time
convenience rather than the primary deterministic workflow.

### Lockfile-only images

If you do not want to ship vendored artifacts, keep `gestalt.lock.json` in the
image but exclude `.gestaltd/` from git and from the Docker build context:

```gitignore
deploy/.gestaltd/
```

```dockerignore
deploy/.gestaltd
```

Then make sure the configured artifacts directory is writable by the runtime
user. For example:

```dockerfile
FROM valontechnologies/gestaltd:latest-alpine
USER root
COPY --chown=nobody:nobody deploy/ /app/
USER nobody
CMD ["serve", "--locked", "--config", "/app/config.yaml"]
```

This model treats `.gestaltd/` as a runtime cache. On ephemeral platforms,
artifacts may be redownloaded on cold start.

## Compose example

```yaml
services:
  gestaltd:
    image: valontechnologies/gestaltd:latest
    ports:
      - "8080:8080"
      - "127.0.0.1:9090:9090"
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
- otherwise, config loading fails unless you used `${VAR:-}` for an explicit empty default

That means this works well with Docker secrets:

```yaml
server:
  encryptionKey: ${GESTALT_ENCRYPTION_KEY}
```

```sh
docker run --rm \
  -p 8080:8080 \
  -v "$(pwd)/gestalt.yaml:/etc/gestalt/config.yaml:ro" \
  -v /run/secrets/gestalt-encryption-key:/run/secrets/gestalt-encryption-key:ro \
  -e GESTALT_ENCRYPTION_KEY_FILE=/run/secrets/gestalt-encryption-key \
  valontechnologies/gestaltd:latest
```

For more advanced setups, Gestalt also supports `secret://...` references. The built-in `env` and `file` providers are always available. Cloud secret backends (Google Secret Manager, AWS Secrets Manager, HashiCorp Vault, Azure Key Vault) are available as external providers from [gestalt-providers](https://github.com/valon-technologies/gestalt-providers). See the [secrets documentation](https://gestaltd.ai/providers/secrets) for configuration details.

## Health endpoints

The container exposes:

- `GET /health` for liveness
- `GET /ready` for readiness

The built-in admin UI is served from `/admin`. The public client UI at `/` is
controlled by `ui.provider`: set `ui.provider: none` to disable it, omit `ui`
to use the pinned first-party default bundle, or point `ui.provider.source` at
your own packaged UI. If you configure `server.management`, then `/admin`,
`/metrics`, `/health`, and `/ready` move to the management listener instead.
That split is the recommended production deployment shape; the single-listener
mode is mainly for local development and trusted-network use.

## SQLite and `/data`

SQLite works well for:

- local development
- demos
- single-instance deployments with persistent storage

If you choose SQLite, store the database on mounted durable storage such as `/data/gestalt.db`.

For horizontally scaled deployments, prefer Postgres or MySQL.

## Debugging

The default static image does not include a shell. Use the `-alpine` variant
for interactive debugging:

```sh
docker run --rm -it --entrypoint sh valontechnologies/gestaltd:latest-alpine
```

To check startup behavior:

```sh
docker run --rm valontechnologies/gestaltd:latest --help
```

## Releasing providers

If you build a provider release in Docker, run `gestaltd provider release` from
the provider source directory:

```dockerfile
FROM valontechnologies/gestaltd:latest-alpine AS gestaltd

FROM golang:1.26-alpine AS build
RUN apk add --no-cache git
COPY --from=gestaltd /gestaltd /usr/local/bin/gestaltd
WORKDIR /src
COPY . .
RUN cd ./my-plugin && \
    gestaltd provider release --version 0.0.1-alpha.1 --platform all && \
    gestaltd init --config ./deploy/config.yaml
```

## Caveats

- The published image defaults to locked startup. A missing config file, missing lockfile, missing verified archive hash, or unwritable artifacts directory causes startup to fail fast. Missing prepared artifacts alone do not, because `gestaltd` can materialize them from `gestalt.lock.json`.
- `docker run valontechnologies/gestaltd:latest` by itself is expected to fail because the image does not auto-generate config in-container.
- The default image does not include a shell. Use `-alpine` or `-debian` for debugging.
- If you use SQLite, do not scale to multiple replicas.

## Learn more

- Docs: https://gestaltd.ai
- CLI reference: https://gestaltd.ai/reference/cli
- Deployment docs: https://gestaltd.ai/deploy
- Source: https://github.com/valon-technologies/gestalt
