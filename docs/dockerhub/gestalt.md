# gestalt Docker image

`gestalt` is a CLI client for the Gestalt API. It connects to a running
`gestaltd` server and provides commands for:

- authenticating via OAuth or API tokens
- listing and connecting third-party integrations
- invoking integration operations
- managing configuration and credentials

## Quick reference

- Image: `valontechnologies/gestalt`
- Entrypoint: `/gestalt`
- This image runs the `gestalt` CLI on a distroless base. There is no shell.

## Supported tags

- `latest`
- `<version>`

The image is published for `linux/amd64` and `linux/arm64`.

## What the image includes

The image uses `gcr.io/distroless/cc-debian12` as its base and contains the
`gestalt` binary on a minimal distroless runtime with CA certificates. It runs
as `nonroot:nonroot`.

## Usage

Point the CLI at a running `gestaltd` server:

```sh
docker run --rm \
  -e GESTALT_URL=https://gestalt.example.com \
  -e GESTALT_API_KEY=gst_... \
  valontechnologies/gestalt:latest \
  integrations list
```

### Invoke an operation

```sh
docker run --rm \
  -e GESTALT_URL=https://gestalt.example.com \
  -e GESTALT_API_KEY=gst_... \
  valontechnologies/gestalt:latest \
  invoke github search_code -p "query=gestalt org:my-org"
```

### Use in CI pipelines

The Docker image works well in CI environments where installing the native
binary is inconvenient:

```yaml
jobs:
  check-integrations:
    runs-on: ubuntu-latest
    steps:
      - run: |
          docker run --rm \
            -e GESTALT_URL="${{ vars.GESTALT_URL }}" \
            -e GESTALT_API_KEY="${{ secrets.GESTALT_API_KEY }}" \
            valontechnologies/gestalt:latest \
            integrations list --format json
```

## Available commands

| Command                     | Description                            |
|-----------------------------|----------------------------------------|
| `auth login`                | Log in via browser OAuth flow          |
| `auth logout`               | Log out and clear stored credentials   |
| `auth status`               | Show authentication status             |
| `init`                      | Interactive setup wizard               |
| `config get/set/unset/list` | Manage persistent configuration        |
| `integrations list`         | List available integrations            |
| `integrations connect`      | Connect an integration via OAuth or interactive manual auth |
| `invoke <integ> <op>`       | Execute an integration operation       |
| `describe <integ> <op>`     | Describe an integration operation      |
| `tokens create/list/revoke` | Manage API tokens                      |

Use `--format json` or `--format table` to control output format.

## Environment variables

| Variable          | Description                     |
|-------------------|---------------------------------|
| `GESTALT_URL`     | Base URL of the gestaltd server |
| `GESTALT_API_KEY` | API token for authentication    |

## Debugging

The image is distroless and does not include a shell. Use the `--help` flag
to inspect available commands:

```sh
docker run --rm valontechnologies/gestalt:latest --help
docker run --rm valontechnologies/gestalt:latest invoke --help
```

For an interactive shell, use the native binary or Homebrew install instead
of the Docker image.

## Learn more

- Docs: https://gestaltd.ai
- CLI reference: https://gestaltd.ai/reference/cli
- Source: https://github.com/valon-technologies/gestalt
