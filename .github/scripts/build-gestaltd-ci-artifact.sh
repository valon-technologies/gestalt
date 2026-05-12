#!/usr/bin/env bash
set -euo pipefail

source_dir="${GESTALTD_SOURCE_DIR:-$(git rev-parse --show-toplevel)}"
source_dir="$(cd "${source_dir}" && pwd)"
commit="${GESTALTD_COMMIT:?GESTALTD_COMMIT is required}"
version="${GESTALTD_VERSION:?GESTALTD_VERSION is required}"
output_dir="${GESTALTD_OUTPUT_DIR:-${source_dir}/dist/gestaltd-ci}"
artifact_base_url="${GESTALTD_ARTIFACT_BASE_URL:-}"
run_id="${GITHUB_RUN_ID:-local}"
created_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

if [[ ! "${commit}" =~ ^[0-9a-f]{40}$ ]]; then
  echo "GESTALTD_COMMIT must be a full lowercase 40-character commit SHA." >&2
  exit 1
fi

if [[ "${output_dir}" != /* ]]; then
  output_dir="${PWD}/${output_dir}"
fi

rm -rf "${output_dir}"
mkdir -p "${output_dir}/build"

(
  cd "${source_dir}/gestaltd"
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags "-X main.version=${version}" \
    -o "${output_dir}/build/gestaltd" \
    ./cmd/gestaltd
)

if [[ "$(uname -s)" == "Linux" && "$(uname -m)" == "x86_64" ]]; then
  version_output="$("${output_dir}/build/gestaltd" version)"
  if [[ "${version_output}" != "${version}" ]]; then
    echo "gestaltd version output '${version_output}' did not match expected '${version}'." >&2
    exit 1
  fi
fi

if [[ "${GESTALTD_SKIP_ALPINE_SMOKE:-}" != "1" ]]; then
  docker run --rm \
    -v "${output_dir}/build:/build:ro" \
    alpine:3.23 \
    /build/gestaltd version > "${output_dir}/alpine-version.txt"
  alpine_version="$(cat "${output_dir}/alpine-version.txt")"
  if [[ "${alpine_version}" != "${version}" ]]; then
    echo "Alpine gestaltd version output '${alpine_version}' did not match expected '${version}'." >&2
    exit 1
  fi
fi

archive="gestaltd-linux-x86_64.tar.gz"
tar -czf "${output_dir}/${archive}" -C "${output_dir}/build" gestaltd
(
  cd "${output_dir}"
  sha256sum "${archive}" > "${archive}.sha256"
)

archive_sha256="$(awk '{print $1}' "${output_dir}/${archive}.sha256")"

export GESTALTD_METADATA_ARTIFACT_BASE_URL="${artifact_base_url}"
export GESTALTD_METADATA_ARCHIVE="${archive}"
export GESTALTD_METADATA_ARCHIVE_SHA256="${archive_sha256}"
export GESTALTD_METADATA_COMMIT="${commit}"
export GESTALTD_METADATA_CREATED_AT="${created_at}"
export GESTALTD_METADATA_OUTPUT="${output_dir}/metadata.json"
export GESTALTD_METADATA_RUN_ID="${run_id}"
export GESTALTD_METADATA_VERSION="${version}"

python3 - <<'PY'
import json
import os

base_url = os.environ["GESTALTD_METADATA_ARTIFACT_BASE_URL"].rstrip("/")
archive = os.environ["GESTALTD_METADATA_ARCHIVE"]
archive_url = f"{base_url}/{archive}" if base_url else None

metadata = {
    "schema": "gestaltd-ci-artifact",
    "schemaVersion": 1,
    "repository": "github.com/valon-technologies/gestalt",
    "commit": os.environ["GESTALTD_METADATA_COMMIT"],
    "version": os.environ["GESTALTD_METADATA_VERSION"],
    "createdAt": os.environ["GESTALTD_METADATA_CREATED_AT"],
    "createdBy": "github-actions",
    "githubRunID": os.environ["GESTALTD_METADATA_RUN_ID"],
    "artifacts": {
        "linux/amd64": {
            "path": archive,
            "sha256": os.environ["GESTALTD_METADATA_ARCHIVE_SHA256"],
            "url": archive_url,
            "cgoEnabled": False,
        },
    },
}

with open(os.environ["GESTALTD_METADATA_OUTPUT"], "w", encoding="utf-8") as f:
    json.dump(metadata, f, indent=2, sort_keys=True)
    f.write("\n")
PY

find "${output_dir}" -maxdepth 1 -type f -print | sort
