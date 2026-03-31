#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "${ROOT}/sdk/pluginsdk/proto"

if command -v buf &>/dev/null; then
  buf generate
else
  echo "buf is not installed — falling back to protoc (Go only)."
  echo "Install buf from https://buf.build/docs/installation for multi-language generation."
  echo ""

  export PATH="$(go env GOPATH)/bin:${PATH}"
  cd "${ROOT}"

  protoc \
    --go_out=. \
    --go_opt=paths=source_relative \
    --go-grpc_out=. \
    --go-grpc_opt=paths=source_relative \
    sdk/pluginsdk/proto/v1/plugin.proto
fi
