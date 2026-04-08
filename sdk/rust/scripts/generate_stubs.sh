#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
crate_dir="$repo_root/sdk/rust"

cd "$crate_dir"

cargo clean
cargo test --test codegen

echo "Generated bindings live under $crate_dir/target and should not be committed."
