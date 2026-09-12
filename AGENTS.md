# AGENTS.md

## Cursor Cloud specific instructions

This repo is the **Gestalt** monorepo: a Go server daemon (`gestaltd`), a Rust CLI
(`gestalt`), and SDKs (`sdk/go`, `sdk/python`, `sdk/rust`, `sdk/typescript`), plus a
Next.js docs site (`docs`). Standard per-component build/test/lint commands live in
`CONTRIBUTING.md` — use it as the source of truth and only the non-obvious caveats
below.

### Toolchains (already installed in the VM snapshot)
- Go: the base `go` launcher on PATH is older (1.22.2), but the repo requires **Go
  1.26.4**. `GOTOOLCHAIN=go1.26.4` is persisted via `go env -w` (`~/.config/go/env`),
  so every `go` invocation (including build subprocesses) uses the cached 1.26.4
  toolchain automatically. Do **not** unset this — without it, standalone modules whose
  `go.mod` says `go 1.26` try to download a nonexistent exact `go1.26` toolchain and fail.
- Rust: `rustup default` is `stable` (1.96+). The repo uses edition 2024, so the
  preinstalled-image default of 1.83 is too old; stable is already selected.
- `golangci-lint` v2.11.3 is on PATH (`/usr/local/bin`).
- Python SDK tooling: `uv` (`~/.local/bin/uv`); TypeScript SDK tooling: `bun`
  (`~/.bun/bin/bun`). Both are on PATH in login shells.
- `python3-venv` (system package) is installed; the Go test
  `TestLoadForExecutionAtPath_GeneratesStaticCatalogForLocalPythonSourcePlugin`
  needs it to create venvs.
- `buf` is **not** installed and is **not** needed to build/test/run — generated
  protobuf bindings are committed. It is only required to regenerate proto bindings.

### gestalt CLI (`gestalt`) dependency caveat
`gestalt/Cargo.lock` is git-ignored, so a fresh resolve picks `time` 0.3.52, which is
incompatible with `cookie` 0.18.1 (a transitive dep of `reqwest`'s cookie store) and
fails to compile. Pin it back: `cd gestalt && cargo update -p time --precise 0.3.47`
(any 0.3.48+ breaks the build). The startup update script does this automatically.

### Running the product end-to-end (hello world)
The documented `gestaltd` auto-config flow (run `gestaltd` with no args) currently
**fails** against the published `gestalt-providers` releases: the `httpbin` (and other)
release manifests predate the `staticValidation.manifest` field that current `main`
requires. For a self-contained local end-to-end run, use the synthesized baseline mode
with a local source app instead (the `indexeddb` and `externalcredentials` release
providers it pulls do load fine):

1. Build the server: `cd gestaltd && go build -o /tmp/gestaltd ./cmd/gestaltd`.
2. Copy the example app fixture to a writable dir and rewrite its `go.mod` `replace`
   paths to absolute (`/workspace/sdk/go`, `/workspace/gestaltd/rpc`); its committed
   `manifest.yaml` is stale (declares `entrypoint.artifactPath`, which current code
   rejects for source manifests) so give it a `run:` block instead, e.g.
   `run: { command: [sh, -c, "sh ./build.sh && exec ./.gestalt/build/provider"] }`.
   Source: `gestaltd/internal/testutil/testdata/provider-go`.
3. Start it: `/tmp/gestaltd serve <app-dir> --port 8080` (builds the local Go provider,
   serves on `http://127.0.0.1:8080`; admin UI at `/admin/`, MCP at `/mcp`).
4. Drive it with the CLI (`cd gestalt && cargo build`, binary at
   `target/debug/gestalt`): `gestalt --url http://127.0.0.1:8080 app list` then
   `gestalt --url http://127.0.0.1:8080 app invoke provider_go greet -p name=World`
   (returns `Hello, World!`).

### Known environment limitation
The 5 `internal/bootstrap` sandbox tests (`TestSandboxed*`) fail because the Firecracker
VM kernel lacks Landlock LSM support ("missing kernel Landlock support. Got Landlock ABI
v0"). This is a kernel limitation, not a code problem; all other `gestaltd` tests pass.
