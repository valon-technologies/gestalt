#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
proto_root="$repo_root/sdk/proto"
generated_root="$repo_root/sdk/rust/src/generated"

"$proto_root/scripts/buf_generate.sh" rust

if command -v rustfmt >/dev/null 2>&1; then
  find "$generated_root" -name '*.rs' -print0 | xargs -0 rustfmt --edition 2024
fi
