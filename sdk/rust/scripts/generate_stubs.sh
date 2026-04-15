#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

CARGO_TARGET_DIR="$repo_root/sdk/rust/target" \
  cargo run --quiet --locked --manifest-path "$repo_root/tools/rust-sdk-codegen/Cargo.toml"
