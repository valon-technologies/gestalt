#!/usr/bin/env bash

set -euo pipefail

if ! command -v buf >/dev/null 2>&1; then
  echo "buf is required to verify generated Go SDK stubs" >&2
  exit 1
fi

repo_root=$(git rev-parse --show-toplevel)

"$repo_root/sdk/proto/update-go-sdk.sh"

status_output=$(git -C "$repo_root" status --short --untracked-files=all -- sdk/go/gen)

if [[ -n "$status_output" ]]; then
  echo "Generated Go SDK stubs are out of sync with sdk/proto." >&2
  echo "Run 'sdk/proto/update-go-sdk.sh' and commit the updated files under sdk/go/gen." >&2
  printf '%s\n' "$status_output" >&2
  git -C "$repo_root" --no-pager diff -- sdk/go/gen
  exit 1
fi
