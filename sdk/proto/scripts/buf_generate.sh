#!/usr/bin/env bash

set -euo pipefail

readonly BUF_VERSION="1.66.1"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
proto_root="$(cd "$script_dir/.." && pwd)"

usage() {
  echo "usage: $0 <go|python|rust|typescript|template.yaml> [buf generate args...]" >&2
}

if [[ $# -lt 1 ]]; then
  usage
  exit 2
fi

target="$1"
shift

case "$target" in
  go | python | rust | typescript)
    template="buf.$target.gen.yaml"
    ;;
  *.yaml)
    template="$target"
    ;;
  *)
    usage
    exit 2
    ;;
esac

if [[ ! -f "$proto_root/$template" ]]; then
  echo "Buf template not found: $proto_root/$template" >&2
  exit 1
fi

buf_cmd=()
found_buf_version=""
if command -v buf >/dev/null 2>&1; then
  found_buf_version="$(buf --version)"
fi

if [[ "$found_buf_version" == "$BUF_VERSION" ]]; then
  buf_cmd=(buf)
elif command -v go >/dev/null 2>&1; then
  buf_cmd=(go run "github.com/bufbuild/buf/cmd/buf@v$BUF_VERSION")
elif [[ -n "$found_buf_version" ]]; then
  echo "buf $BUF_VERSION or Go is required to run Buf generation; found buf $found_buf_version" >&2
  exit 1
else
  echo "buf $BUF_VERSION or Go is required to run Buf generation" >&2
  exit 1
fi

(cd "$proto_root" && "${buf_cmd[@]}" generate --template "$template" "$@")
