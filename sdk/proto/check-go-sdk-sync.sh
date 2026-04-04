#!/usr/bin/env bash

set -euo pipefail

if ! command -v buf >/dev/null 2>&1; then
  echo "buf is required to verify generated Go SDK stubs" >&2
  exit 1
fi

repo_root=$(git rev-parse --show-toplevel)

(
  cd "$repo_root/sdk/proto"
  buf generate
)

if ! git -C "$repo_root" diff --quiet -- sdk/go/gen; then
  echo "Generated Go SDK stubs are out of sync with sdk/proto." >&2
  echo "Run 'cd sdk/proto && buf generate' and commit the updated files under sdk/go/gen." >&2
  git -C "$repo_root" --no-pager diff -- sdk/go/gen
  exit 1
fi
