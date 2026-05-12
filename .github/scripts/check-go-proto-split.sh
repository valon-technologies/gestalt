#!/usr/bin/env bash
set -euo pipefail

repo_root=$(git rev-parse --show-toplevel)
module_dir=${1:-"$repo_root/gestaltd"}

server_proto="github.com/valon-technologies/gestalt/server/internal/gen/v1"
sdk_proto="github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
sdk_public_proto="github.com/valon-technologies/gestalt/sdk/go/gen/v1"
deleted_root_proto="github.com/valon-technologies/gestalt/internal"
deleted_root_proto="$deleted_root_proto/gen/v1"

if git -C "$repo_root" grep --line-number --fixed-strings "$deleted_root_proto" -- . \
	':!.github/scripts/check-go-proto-split.sh'; then
	cat >&2 <<EOF
Tracked files must not reference the deleted root Go module path:
  $deleted_root_proto

Use Buf managed go_package overrides in sdk/proto/buf.go.*.gen.yaml instead.
EOF
	exit 1
fi

for codec_dir in sdk/go/internal/indexeddbcodec gestaltd/internal/indexeddbcodec; do
	for generated_proto in "$server_proto" "$sdk_proto" "$sdk_public_proto"; do
		if git -C "$repo_root" grep --line-number --fixed-strings "$generated_proto" -- "$codec_dir"; then
			cat >&2 <<EOF
IndexedDB codec packages must not import generated Gestalt protobuf packages:
  $codec_dir

Keep generated protobuf adapters at the SDK/server transport boundary instead.
EOF
			exit 1
		fi
	done
done

if git -C "$repo_root" grep --line-number --fixed-strings "proto." -- sdk/go/indexeddb_codec.go; then
	cat >&2 <<EOF
sdk/go/indexeddb_codec.go is the public native/byte IndexedDB codec surface and
must not expose generated protobuf types.

Move generated protobuf helpers to private transport adapter files instead.
EOF
	exit 1
fi

cd "$module_dir"

failures=$(
	go list -test -deps -f '{{.ImportPath}} {{join .Deps " "}}' ./... |
		awk -v server="$server_proto" -v sdk="$sdk_proto" '
			index($0, server) && index($0, sdk) { print $1 }
		' |
		sort -u
)

if [ -n "$failures" ]; then
	cat >&2 <<EOF
The following gestaltd package/test dependency graphs import both generated Go
protobuf packages:

$failures

Do not link these packages into the same Go binary. Server code should use:
  $server_proto

Provider SDK code should use:
  $sdk_proto
EOF
	exit 1
fi
