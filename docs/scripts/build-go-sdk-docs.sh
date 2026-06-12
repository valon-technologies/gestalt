#!/usr/bin/env bash
# Renders the Go SDK API reference as static HTML with the official godoc
# server: serve the module, mirror the package tree (and source views and
# static assets) to disk, and write a root redirect into the package tree.
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <output-dir>" >&2
  exit 2
fi

out="$(mkdir -p "$1" && cd "$1" && pwd)"
module="github.com/valon-technologies/gestalt/sdk/go"
port="${GODOC_PORT:-6060}"

godoc -http="127.0.0.1:${port}" &
godoc_pid=$!
trap 'kill "${godoc_pid}" 2>/dev/null || true' EXIT

ready=""
for _ in $(seq 1 120); do
  if curl -fsS -o /dev/null "http://127.0.0.1:${port}/pkg/${module}/"; then
    ready=1
    break
  fi
  sleep 1
done
if [[ -z "${ready}" ]]; then
  echo "godoc did not serve ${module} within 120s" >&2
  exit 1
fi

rm -rf "${out:?}"/*
(
  cd "${out}"
  # wget's exit codes are noisy for mirrors (404s on side links, source
  # views with query strings); the required artifacts are verified below.
  wget --quiet -e robots=off --mirror --no-host-directories --page-requisites --convert-links \
    --include-directories="/pkg/${module},/src/${module},/lib/godoc" \
    "http://127.0.0.1:${port}/pkg/${module}/" || true
)

test -f "${out}/pkg/${module}/index.html" || {
  echo "mirror missing the module package page" >&2
  exit 1
}
grep -q "func " "${out}/pkg/${module}/client/index.html" || {
  echo "mirror missing the client package rendering" >&2
  exit 1
}

cat > "${out}/index.html" <<HTML
<!doctype html>
<meta http-equiv="refresh" content="0; url=./pkg/${module}/" />
<title>Gestalt Go SDK</title>
<a href="./pkg/${module}/">Gestalt Go SDK API reference</a>
HTML
