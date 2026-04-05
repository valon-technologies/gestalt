#!/usr/bin/env bash

set -euo pipefail

if ! command -v buf >/dev/null 2>&1; then
  echo "buf is required to regenerate Go SDK stubs" >&2
  exit 1
fi

repo_root=$(git rev-parse --show-toplevel)

cd "$repo_root/sdk/proto"
buf generate
