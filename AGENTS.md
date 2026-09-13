# Agent Rules

See `CONTRIBUTING.md` for repository layout and the canonical per-area lint/test/build commands.

## Cursor Cloud specific instructions

These notes apply to the Cursor Cloud VM, where the update script has already fetched Go and Cargo dependencies.

### Toolchain

- The server (`gestaltd`, `sdk/go`) requires **Go 1.26.4** (pinned in `go.work`). The system also has an older Go at
  `/usr/lib/go-1.22`; Go 1.26.4 is installed at `/usr/local/go` and is first on `PATH` (`/usr/local/bin/go`).
- The CLI (`gestalt`) and `sdk/rust` use **Rust edition 2024**, which needs Rust ≥ 1.85. The default `rustup` toolchain
  is set to `stable` (1.96+). The base image's older `cargo` (1.83) cannot build this repo.

### Building nested provider modules

- The example/provider fixtures under `gestaltd/internal/testutil/testdata/` (e.g. `provider-go`,
  `provider-go-indexeddb`) have their own `go.mod` with relative `replace` directives into `sdk/go` and `gestaltd/rpc`.
  Building them while the repo `go.work` is active fails ("main module does not contain package ..."). Set `GOWORK=off`
  when building these nested modules.

### Running the server locally

- The README "zero-config" `gestaltd` flow and `gestaltd serve --path` currently fail in an isolated environment: they
  fetch first-party provider releases from `github.com/valon-technologies/gestalt-providers`, and the current server
  enforces a `StaticValidation.Manifest` field (see `gestaltd/internal/providerrelease/release.go`) that those published
  releases do not yet include ("provider release validation manifest is required").
- For a fully-local server, point `GESTALT_PROVIDERS_DIR` at locally-built provider directories (indexeddb,
  externalcredentials, ui) and use local `source: path:` apps. The e2e harness does exactly this; see
  `gestaltd/internal/daemon/testmain_test.go` (`writeDefaultProvidersDir`, `writeLocalProviderReleaseMetadata`) and the
  local-config shape in `gestaltd/internal/daemon/e2e_test.go` (`indexeddb: inmem` with `source: path:` providers).

### Validating end-to-end

- Server (boots the real `gestaltd` with locally built providers and serves the apps API):
  `cd gestaltd && go test ./internal/daemon/ -run TestE2EServeSplitManagementRoutes -count=1`.
- Full server suite: `cd gestaltd && go test ./...`. CLI: `cd gestalt && cargo test --workspace`.
