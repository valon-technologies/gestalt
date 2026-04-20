#!/usr/bin/env bash

set -euo pipefail

readonly GESTALT_PACKAGE_MAINTAINER="Gestalt Maintainers <opensource@valon.com>"
readonly GESTALT_PACKAGE_HOMEPAGE="https://github.com/valon-technologies/gestalt"

debian_arch() {
  local arch="$1"
  case "$arch" in
    amd64|x86_64|linux-x86_64)
      printf 'amd64\n'
      ;;
    arm64|aarch64|linux-arm64)
      printf 'arm64\n'
      ;;
    armhf|armv7|linux-armv7)
      printf 'armhf\n'
      ;;
    *)
      printf 'unsupported Debian architecture: %s\n' "$arch" >&2
      return 1
      ;;
  esac
}

debian_version() {
  local version="$1"
  local marker=
  for marker in alpha beta rc dev; do
    if [[ "$version" == *"-${marker}."* ]]; then
      local base="${version%%-"${marker}".*}"
      local suffix="${version#${base}-}"
      printf '%s~%s-1\n' "$base" "$suffix"
      return 0
    fi
  done
  printf '%s-1\n' "$version"
}

write_sha256_file() {
  local file="$1"
  sha256sum "$file" > "${file}.sha256"
}
