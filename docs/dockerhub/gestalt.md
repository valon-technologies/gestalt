# gestalt Docker image

`gestalt` is a CLI client for the Gestalt API. It connects to a running
`gestaltd` server and provides commands for authenticating, listing plugins,
invoking integration operations, and managing credentials.

> **Alpha.** Gestalt is under active development. Images are tagged
> with alpha versions and may introduce breaking changes. See the
> [documentation](https://gestaltd.ai) for the latest guidance.

## Quick reference

- Image: `valontechnologies/gestalt`
- Entrypoint: `/gestalt`
- This image runs the `gestalt` CLI on a distroless base. There is no shell.

## Supported tags

- `latest`
- `<version>`

The image is published for `linux/amd64`, `linux/arm64`, and `linux/arm/v7`.

## What the image includes

The image uses `gcr.io/distroless/cc-debian12` as its base and contains the
`gestalt` binary on a minimal distroless runtime with CA certificates. It runs
as `nonroot:nonroot`.

## Usage

Point the CLI at a running `gestaltd` server:

```sh
docker run --rm \
  -e GESTALT_URL=https://gestalt.example.com \
  -e GESTALT_API_KEY=gst_api_... \
  valontechnologies/gestalt:latest \
  plugins list
```

### Invoke an operation

```sh
docker run --rm \
  -e GESTALT_URL=https://gestalt.example.com \
  -e GESTALT_API_KEY=gst_api_... \
  valontechnologies/gestalt:latest \
  plugins invoke github search_code -p "query=gestalt org:my-org"
```

### Use in CI pipelines

The Docker image works well in CI environments where installing the native
binary is inconvenient:

```yaml
jobs:
  check-plugins:
    runs-on: ubuntu-latest
    steps:
      - run: |
          docker run --rm \
            -e GESTALT_URL="${{ vars.GESTALT_URL }}" \
            -e GESTALT_API_KEY="${{ secrets.GESTALT_API_KEY }}" \
            valontechnologies/gestalt:latest \
            plugins list --format json
```

## Key commands

| Command | Description |
|---|---|
| `plugins list` | List available plugins |
| `plugins connect NAME` | Connect a plugin via OAuth or manual authentication |
| `plugins disconnect NAME` | Disconnect a plugin |
| `plugins invoke NAME OP` | Execute a plugin operation |
| `tokens create/list/revoke` | Manage API tokens |

Use `--format json` or `--format table` to control output format. See the
[CLI reference](https://gestaltd.ai/reference/cli) for the full command list.

## Environment variables

| Variable | Description |
|---|---|
| `GESTALT_URL` | Base URL of the gestaltd server |
| `GESTALT_API_KEY` | API token for authentication |

## Debugging

The image is distroless and does not include a shell. Use the `--help` flag
to inspect available commands:

```sh
docker run --rm valontechnologies/gestalt:latest --help
docker run --rm valontechnologies/gestalt:latest plugins invoke --help
```

For an interactive shell, use the native binary or Homebrew install instead
of the Docker image.

## Learn more

- Docs: https://gestaltd.ai
- CLI reference: https://gestaltd.ai/reference/cli
- Source: https://github.com/valon-technologies/gestalt
