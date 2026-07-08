# AGENTS.md

## Cursor Cloud specific instructions

Gestalt is a polyglot monorepo. Standard per-component build/lint/test commands live in
`CONTRIBUTING.md` — use those as the source of truth. This section only captures
non-obvious, durable caveats for running things in the Cloud environment.

### Components / services

- `gestaltd` (Go) — the server daemon and the core product. Build: `cd gestaltd && go build ./cmd/gestaltd`.
- `gestalt` (Rust) — CLI client used to drive the server. Build: `cd gestalt && cargo build`.
- `sdk/{go,python,rust,typescript}` — client/provider SDK libraries (optional for core work).
- `docs` (Next.js/Nextra) — documentation site (optional).

There is no root Makefile or docker-compose; each component builds independently. SQLite is
embedded (no external DB process). No auth is required for local runs.

### Toolchain gotchas (important)

- **Go**: the base `go` on PATH is 1.22.2, but the repo needs 1.26.x. `GOTOOLCHAIN=auto` will
  fetch `go1.26.4` on demand for the workspace (`go.work` pins `1.26.4`). When building/serving a
  **local source app** whose `go.mod` says `go 1.26` (no patch), `GOTOOLCHAIN=auto` tries to
  download a nonexistent `go1.26` toolchain and fails — always pin `GOTOOLCHAIN=go1.26.4` in that
  case (e.g. `GOTOOLCHAIN=go1.26.4 go test ./...`).
- **Rust**: the crates use edition 2024 (needs Rust ≥ 1.85). The rustup default may be pinned to an
  older toolchain; run `rustup default stable` (the update script does this). `cargo`/`rustc` must
  report ≥ 1.85 or `cargo build` fails with `feature edition2024 is required`.

### Running the server end-to-end (do NOT rely on zero-config `gestaltd`)

Running bare `gestaltd` (or `gestaltd lock`) uses "full config mode", which resolves and locks
providers from `github.com/valon-technologies/gestalt-providers`. Those published releases are
drifted from `main`: the default home app `app/default` release 404s and older app releases
(e.g. httpbin) lack the validation manifest the current server requires. So the README quickstart
(`gestaltd` with no args → HTTPBin) does not work from a source build against current releases.

For a reliable local run, serve a **local source app** with `gestaltd serve <path>/manifest.yaml`.
The repo ships an example Go-SDK app at `gestaltd/internal/testutil/testdata/provider-go`
(operations: `greet`, `echo`, `status`, ...). Two caveats when serving it directly:

- Its on-disk `manifest.yaml` uses an older format that declares `entrypoint.artifactPath`; current
  source manifests must NOT declare that field (Gestalt derives it). Remove the `entrypoint:` block.
- Its `go.mod` `replace` directives are relative to the repo tree (`../../../../../sdk/go` etc.), so
  copy the app elsewhere only if you rewrite those replaces to absolute paths.

Example that works (server + CLI):

```sh
# copy + adapt the example app (remove entrypoint:, absolute replaces), then:
GOTOOLCHAIN=go1.26.4 ./gestaltd/gestaltd serve /path/to/app/manifest.yaml --port 8080
# in another shell:
./gestalt/target/debug/gestalt --url http://127.0.0.1:8080 app list
./gestalt/target/debug/gestalt --url http://127.0.0.1:8080 app invoke provider_go greet -p name=Cursor
```

Endpoints: HTTP API `GET/POST /api/v1/<app>/<operation>`, admin UI at `/admin/`, MCP at `/mcp`,
default port `8080`. The public root `/` is only served when a UI app is mounted.

### SDK / docs dependency refresh

`bun` and `uv` are installed in the environment. Refresh deps only when lockfiles change:
`sdk/typescript` → `bun install --frozen-lockfile`; `sdk/python` → `uv sync --frozen --group dev`;
`docs` → `npm ci`. The startup update script already runs these guarded.
