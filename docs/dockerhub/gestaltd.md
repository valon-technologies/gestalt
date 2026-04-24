# gestaltd Docker image

`gestaltd` is a platform for self-hostable, configurable integrations and tooling. You describe your platform in one YAML file, and `gestaltd` turns that file into a running server with an HTTP API, admin UI, health endpoints, and optional MCP endpoint.

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

## Supported tags

| Tag | Base | Shell | Size |
| --- | --- | --- | --- |
| `latest`, `<version>` | `scratch` (static) | No | Smallest |
| `latest-alpine`, `<version>-alpine` | Alpine 3.23 | Yes | Small |

All tags are published for `linux/amd64`, `linux/arm64`, and `linux/arm/v7`.

## What the image includes

The default image is a static build: just the `gestaltd` binary and CA
certificates on a `scratch` base. There is no shell, no package manager, and
minimal attack surface.

For debugging or plugin compatibility, use the `-alpine` variant. It includes
a shell, `ca-certificates`, and a writable `/data` directory owned by `nobody`.

## Run a simple config

Mount a config file and writable `/data` volume before starting the container:

```sh
export GESTALT_ENCRYPTION_KEY="$(openssl rand -hex 32)"

docker run --rm \
  -p 8080:8080 \
  -e GESTALT_ENCRYPTION_KEY="${GESTALT_ENCRYPTION_KEY}" \
  -v "$(pwd)/gestalt.yaml:/etc/gestalt/config.yaml:ro" \
  -v gestalt-data:/data \
  valontechnologies/gestaltd:latest
```

Generate the encryption key once with `openssl rand -hex 32` and use that value for the deployment.

Example minimal config:

```yaml
apiVersion: gestaltd.config/v3
server:
  public:
    port: 8080
  encryptionKey: ${GESTALT_ENCRYPTION_KEY}
  providers:
    indexeddb: main

providers:
  indexeddb:
    main:
      source: https://github.com/valon-technologies/gestalt-providers/releases/download/indexeddb/relationaldb/v0.0.1-alpha.4/provider-release.yaml
      config:
        dsn: sqlite:///data/gestalt.db

plugins: {}
```

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
      GESTALT_ENCRYPTION_KEY: "${GESTALT_ENCRYPTION_KEY}"

volumes:
  gestalt-data:
```

## Production images

For deterministic production deployments, run `gestaltd init` locally to
resolve providers and write `gestalt.lock.json`, then bake the result into a
derived image:

```dockerfile
FROM valontechnologies/gestaltd:latest-alpine
COPY deploy/ /app/
CMD ["serve", "--locked", "--config", "/app/config.yaml"]
```

See the [deployment documentation](https://gestaltd.ai/deploy) for the full
vendored-artifacts vs lockfile-only workflow and recommended patterns.

## Configuration and environment variables

Gestalt expands `${VAR}` placeholders in the config before YAML decoding. The
image also supports the `*_FILE` convention: if `VAR` is not set but `VAR_FILE`
is, `${VAR}` resolves to the contents of that file. This works well with
Docker secrets. See the [configuration documentation](https://gestaltd.ai/configuration)
for the full config model and structured secret-ref support.

## Health endpoints

- `GET /health` for liveness
- `GET /ready` for readiness

The admin UI is served at `/admin`. Gestalt uses `server.admin.ui` when set,
otherwise auto-discovers `admin/index.html` from the root `providers.ui`
bundle before falling back to the built-in shell. If you configure
`server.management`, health and admin endpoints move to the management
listener. If you also set `server.admin.authorizationPolicy`, Gestalt applies
browser session authentication and role checks to `/admin`; on split
public/management deployments, set `server.management.baseUrl` so login can
return the browser to the management listener's `/admin` route after callback.
Use the same
hostname as `server.baseUrl`, and keep it on `https` whenever
`server.baseUrl` is `https`, so the session cookie is reusable across both
listeners. See the
[deployment documentation](https://gestaltd.ai/deploy) for the recommended
split-listener production pattern.

The built-in `/admin` shell now includes both the Prometheus metrics dashboard
and a plugin authorization workspace. For any plugin that already declares
`authorizationPolicy`, operators can open `/admin/?tab=members&plugin=<name>`
to inspect merged static/dynamic rows and manage dynamic grants. Static policy
members remain authoritative.

## SQLite and `/data`

SQLite works for local development, demos, and single-instance deployments.
Store the database on a mounted volume (e.g. `/data/gestalt.db`). For
horizontally scaled deployments, use Postgres or MySQL.

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

## Caveats

- The published image defaults to locked startup. A missing config file,
  missing lockfile, or unwritable artifacts directory causes startup to fail
  fast.
- `docker run valontechnologies/gestaltd:latest` by itself fails because the
  image does not auto-generate config in-container.
- The default image does not include a shell. Use `-alpine` for debugging.
- If you use SQLite, do not scale to multiple replicas.

## Learn more

- Docs: https://gestaltd.ai
- CLI reference: https://gestaltd.ai/reference/cli
- Deployment: https://gestaltd.ai/deploy
- Source: https://github.com/valon-technologies/gestalt
