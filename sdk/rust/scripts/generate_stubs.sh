#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
proto_root="$repo_root/sdk/proto"
generated_root="$repo_root/sdk/rust/src/generated"

if ! command -v buf >/dev/null 2>&1; then
  echo "buf is required to regenerate Rust protobuf stubs" >&2
  exit 1
fi

required_buf_version="1.66.1"
buf_version="$(buf --version)"
if [[ "$buf_version" != "$required_buf_version" ]]; then
  echo "buf $required_buf_version is required to regenerate Rust protobuf stubs; found $buf_version" >&2
  exit 1
fi

(cd "$proto_root" && buf generate --template buf.rust.gen.yaml)

if command -v rustfmt >/dev/null 2>&1; then
  find "$generated_root" -name '*.rs' -print0 | xargs -0 rustfmt --edition 2024
fi
