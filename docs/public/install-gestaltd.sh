#!/bin/sh
set -eu

script_url="${GESTALT_INSTALL_SCRIPT_URL:-https://gestaltd.ai/install.sh}"

usage() {
  cat <<'USAGE'
Install gestaltd on Linux or macOS.

Usage:
  install-gestaltd.sh [options]

Options:
  --version VERSION  Release version or gestaltd/v* tag. Default: latest, including prereleases.
  --prefix PATH      Install into PATH/bin. Default: /usr/local/bin.
  --bin-dir PATH     Install directly into PATH.
  --user             Install into $HOME/.local/bin.
  --dry-run          Print the planned install without downloading binaries.
  -h, --help         Show this help.
USAGE
}

download() {
  download_url="$1"
  download_path="$2"

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$download_url" -o "$download_path"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$download_path" "$download_url"
  else
    printf '%s\n' "gestalt installer: curl or wget is required" >&2
    exit 1
  fi
}

for arg in "$@"; do
  case "$arg" in
    -h | --help)
      usage
      exit 0
      ;;
    --component | --component=*)
      printf '%s\n' "gestalt installer: install-gestaltd.sh does not accept --component" >&2
      exit 1
      ;;
  esac
done

tmp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

installer="${tmp_dir}/install.sh"
download "$script_url" "$installer"
GESTALT_INSTALL_COMPONENT=gestaltd sh "$installer" "$@"
