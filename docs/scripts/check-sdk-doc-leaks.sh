#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <python|typescript|rust|docs> <artifact-root>" >&2
  exit 2
fi

mode="$1"
root="$2"

if [[ ! -d "$root" ]]; then
  echo "SDK docs leak check root does not exist: $root" >&2
  exit 1
fi

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

check_files() {
  local pattern="$1"
  shift
  : >"$tmp"
  grep -RInE --include='*.html' --include='*.js' --include='*.json' "$pattern" "$@" >"$tmp" || true
  if [[ -s "$tmp" ]]; then
    echo "SDK docs leak marker matched pattern: $pattern" >&2
    cat "$tmp" >&2
    exit 1
  fi
}

case "$mode" in
  python)
    check_files 'gestalt\.protocol\.v1|gestalt\.testing|_pb2|pb2_grpc|protobuf|generated (protobuf|protocol|modules)|interoperability fixtures|ENV_[A-Z0-9_]*SOCKET(_TOKEN)?|_modules/|View Source|proto_dict|indexeddb_.*_proto' "$root"
    ;;
  typescript)
    check_files 'protocol/v1|protobuf|generated (schemas|protocol|modules)|Regenerating protobuf|buf generate|ENV_[A-Z0-9_]*SOCKET(_TOKEN)?|Defined in |buildProviderBinary|parseRuntimeArgs|runBundledProvider|loadProviderFromTarget|parseProviderTarget' "$root"
    ;;
  rust)
    files=()
    [[ -f "$root/index.html" ]] && files+=("$root/index.html")
    [[ -f "$root/sidebar-items.js" ]] && files+=("$root/sidebar-items.js")
    if [[ ${#files[@]} -eq 0 ]]; then
      echo "No rustdoc root files found under $root" >&2
      exit 1
    fi
    check_files 'proto::v1|sdk/proto|src/generated|Codegen strategy|protobuf|generated protocol|gRPC bindings|ENV_[A-Z0-9_]*SOCKET_TOKEN' "${files[@]}"
    ;;
  docs)
    check_files 'Recommended path|Use a Gestalt SDK when|/reference/sdk/(python|typescript|go|rust)|generated oneof|protobuf|protocol-backed' "$root"
    ;;
  *)
    echo "unknown SDK docs leak check mode: $mode" >&2
    exit 2
    ;;
esac
