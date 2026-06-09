#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
buf_bin="${BUF_BIN:-buf}"
if ! command -v "$buf_bin" >/dev/null 2>&1; then
  gopath_buf="$(go env GOPATH)/bin/buf"
  if [[ -x "$gopath_buf" ]]; then
    buf_bin="$gopath_buf"
  fi
fi

image_path="$(mktemp)"
trap 'rm -f "$image_path"' EXIT

cd "$repo_root/sdk/proto"
"$buf_bin" generate --template buf.go.sdk.gen.yaml --path v1/authorization.proto
"$buf_bin" build . \
  --path v1/authorization.proto \
  --as-file-descriptor-set \
  -o "$image_path"

cd "$repo_root/sdk/proto"
"$buf_bin" generate --template buf.typescript.gen.yaml --path v1/authorization.proto .

cd "$repo_root/sdk/python"
BUF_BIN="$buf_bin" GESTALT_PROTO_MODULES=authorization uv run python scripts/generate_stubs.py

cd "$repo_root/sdk/rust"
BUF_BIN="$buf_bin" ./scripts/generate_stubs.sh

cd "$repo_root/gestaltd"
go run ./tools/sdkwrapgen \
  -config ../sdk/sdkgen.yaml \
  -image "$image_path" \
  -out-root "$repo_root"
