#!/bin/sh
set -eu

repo="valon-technologies/gestalt"
component="both"
version="latest"
prefix="/usr/local"
prefix_set=0
bin_dir=""
user_install=0
dry_run=0
path_flags=0

default_releases_url="https://api.github.com/repos/${repo}/releases?per_page=100&page={page}"
releases_url="${GESTALT_INSTALL_RELEASES_URL:-$default_releases_url}"
download_base="${GESTALT_INSTALL_DOWNLOAD_BASE:-https://github.com/${repo}/releases/download}"
max_release_pages="${GESTALT_INSTALL_MAX_PAGES:-5}"

usage() {
  cat <<'USAGE'
Install Gestalt on Linux.

Usage:
  install.sh [options]

Options:
  --component both|gestalt|gestaltd  Component to install. Default: both.
  --version VERSION                  Release version or family-prefixed tag. Default: latest, including prereleases.
  --prefix PATH                      Install into PATH/bin. Default: /usr/local/bin.
  --bin-dir PATH                     Install directly into PATH.
  --user                             Install into $HOME/.local/bin.
  --dry-run                          Print the planned install without downloading binaries.
  -h, --help                         Show this help.
USAGE
}

log() {
  printf '%s\n' "$*"
}

fail() {
  printf 'gestalt installer: %s\n' "$*" >&2
  exit 1
}

need_value() {
  if [ "$#" -lt 2 ] || [ -z "${2:-}" ]; then
    fail "$1 requires a value"
  fi
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --component)
      need_value "$@"
      component="$2"
      shift 2
      ;;
    --version)
      need_value "$@"
      version="$2"
      shift 2
      ;;
    --prefix)
      need_value "$@"
      prefix="$2"
      prefix_set=1
      path_flags=$((path_flags + 1))
      shift 2
      ;;
    --bin-dir)
      need_value "$@"
      bin_dir="$2"
      path_flags=$((path_flags + 1))
      shift 2
      ;;
    --user)
      user_install=1
      path_flags=$((path_flags + 1))
      shift
      ;;
    --dry-run)
      dry_run=1
      shift
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      fail "unknown option: $1"
      ;;
  esac
done

case "$component" in
  both | gestalt)
    release_family="gestalt"
    ;;
  gestaltd)
    release_family="gestaltd"
    ;;
  *)
    fail "--component must be one of: both, gestalt, gestaltd"
    ;;
esac

if [ "$path_flags" -gt 1 ]; then
  fail "--bin-dir, --prefix, and --user are mutually exclusive"
fi

if [ "$user_install" -eq 1 ]; then
  [ -n "${HOME:-}" ] || fail "--user requires HOME to be set"
  bin_dir="${HOME}/.local/bin"
elif [ -n "$bin_dir" ]; then
  :
elif [ "$prefix_set" -eq 1 ]; then
  bin_dir="${prefix}/bin"
else
  bin_dir="/usr/local/bin"
fi

lower() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

detect_platform() {
  os_name="$(lower "${GESTALT_INSTALL_OS:-$(uname -s)}")"
  arch_name="$(lower "${GESTALT_INSTALL_ARCH:-$(uname -m)}")"

  case "$os_name" in
    linux)
      os_part="linux"
      ;;
    *)
      fail "this installer supports Linux; use Homebrew or binary releases for ${os_name}"
      ;;
  esac

  case "$arch_name" in
    x86_64 | amd64)
      arch_part="x86_64"
      ;;
    aarch64 | arm64)
      arch_part="arm64"
      ;;
    armv7 | armv7l)
      arch_part="armv7"
      ;;
    *)
      fail "unsupported Linux architecture: ${arch_name}"
      ;;
  esac

  platform="${os_part}-${arch_part}"
}

download() {
  download_url="$1"
  download_path="$2"

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$download_url" -o "$download_path"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$download_path" "$download_url"
  else
    fail "curl or wget is required"
  fi
}

release_page_url() {
  page_number="$1"
  case "$releases_url" in
    *"{page}"*)
      page_prefix=${releases_url%%\{page\}*}
      page_suffix=${releases_url#*\{page\}}
      printf '%s%s%s\n' "$page_prefix" "$page_number" "$page_suffix"
      ;;
    *"?"*)
      printf '%s&page=%s\n' "$releases_url" "$page_number"
      ;;
    *)
      printf '%s?page=%s\n' "$releases_url" "$page_number"
      ;;
  esac
}

extract_matching_tag() {
  release_file="$1"
  tag_prefix="$2"

  awk -v tag_prefix="$tag_prefix" '
    /"tag_name"[[:space:]]*:/ {
      tag_line = $0
      sub(/^.*"tag_name"[[:space:]]*:[[:space:]]*"/, "", tag_line)
      sub(/".*$/, "", tag_line)
      tag = tag_line
    }
    /"draft"[[:space:]]*:/ {
      draft_line = $0
      sub(/^.*"draft"[[:space:]]*:[[:space:]]*/, "", draft_line)
      sub(/[,}].*$/, "", draft_line)
      gsub(/[[:space:]]/, "", draft_line)
      if (tag ~ "^" tag_prefix && draft_line != "true") {
        print tag
        exit
      }
      tag = ""
    }
  ' "$release_file"
}

resolve_latest_version() {
  tag_prefix="${release_family}/v"
  page=1

  while [ "$page" -le "$max_release_pages" ]; do
    page_url="$(release_page_url "$page")"
    release_file="${tmp_dir}/releases-${page}.json"
    download "$page_url" "$release_file" || fail "failed to fetch GitHub releases page ${page}"
    matching_tag="$(extract_matching_tag "$release_file" "$tag_prefix")"
    if [ -n "$matching_tag" ]; then
      printf '%s\n' "${matching_tag#"$tag_prefix"}"
      return
    fi
    page=$((page + 1))
  done

  fail "no ${tag_prefix} release found after ${max_release_pages} pages; pass --version VERSION to install a specific release"
}

normalize_version() {
  requested="$1"
  family="$2"

  case "$requested" in
    latest)
      resolve_latest_version
      ;;
    "${family}/v"*)
      printf '%s\n' "${requested#"${family}/v"}"
      ;;
    */v*)
      fail "--version ${requested} does not match --component ${component}; expected ${family}/v*"
      ;;
    v*)
      printf '%s\n' "${requested#v}"
      ;;
    *)
      printf '%s\n' "$requested"
      ;;
  esac
}

sha256_actual() {
  archive_path="$1"
  sha_tool="${GESTALT_INSTALL_SHA256_TOOL:-}"

  case "$sha_tool" in
    none)
      fail "no supported SHA-256 tool found"
      ;;
    sha256sum | "")
      if [ "$sha_tool" = "sha256sum" ] && ! command -v sha256sum >/dev/null 2>&1; then
        fail "sha256sum is not available"
      fi
      if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$archive_path" | awk '{print $1}'
        return
      fi
      ;;
    shasum)
      if command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$archive_path" | awk '{print $1}'
        return
      fi
      fail "shasum is not available"
      ;;
    *)
      fail "unsupported GESTALT_INSTALL_SHA256_TOOL: ${sha_tool}"
      ;;
  esac

  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$archive_path" | awk '{print $1}'
    return
  fi

  fail "no supported SHA-256 tool found"
}

verify_checksum() {
  archive_path="$1"
  archive_name="$2"
  checksum_path="$3"

  expected_hash="$(awk 'NF >= 1 { print $1; exit }' "$checksum_path")"
  expected_name="$(awk 'NF >= 2 { print $2; exit }' "$checksum_path")"

  case "$expected_hash" in
    "" | *[!0123456789abcdefABCDEF]*)
      fail "checksum file is malformed: ${checksum_path}"
      ;;
  esac

  if [ "$expected_name" != "$archive_name" ]; then
    fail "checksum file is for ${expected_name:-<missing>}, expected ${archive_name}"
  fi

  actual_hash="$(sha256_actual "$archive_path")"
  expected_hash="$(printf '%s' "$expected_hash" | tr '[:upper:]' '[:lower:]')"
  actual_hash="$(printf '%s' "$actual_hash" | tr '[:upper:]' '[:lower:]')"

  if [ "$actual_hash" != "$expected_hash" ]; then
    fail "checksum mismatch for ${archive_name}"
  fi
}

expected_binaries() {
  case "$component" in
    both)
      printf '%s\n' "gestalt gestaltd"
      ;;
    gestalt)
      printf '%s\n' "gestalt"
      ;;
    gestaltd)
      printf '%s\n' "gestaltd"
      ;;
  esac
}

ensure_install_dir() {
  if [ -d "$bin_dir" ] && [ -w "$bin_dir" ]; then
    sudo_cmd=""
    return
  fi

  if mkdir -p "$bin_dir" 2>/dev/null && [ -w "$bin_dir" ]; then
    sudo_cmd=""
    return
  fi

  if command -v sudo >/dev/null 2>&1; then
    sudo mkdir -p "$bin_dir"
    sudo_cmd="sudo"
    return
  fi

  fail "cannot write to ${bin_dir}; rerun with --user or --bin-dir PATH"
}

install_binary() {
  source_path="$1"
  binary_name="$2"
  target_path="${bin_dir}/${binary_name}"

  if [ -n "$sudo_cmd" ]; then
    $sudo_cmd install -m 0755 "$source_path" "$target_path"
  else
    install -m 0755 "$source_path" "$target_path"
  fi
}

warn_path() {
  case ":${PATH:-}:" in
    *":${bin_dir}:"*)
      ;;
    *)
      log "Warning: ${bin_dir} is not on PATH."
      ;;
  esac
}

tmp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

detect_platform
resolved_version="$(normalize_version "$version" "$release_family")"
release_tag="${release_family}/v${resolved_version}"
archive_name="${release_family}-${platform}.tar.gz"
archive_url="${download_base}/${release_tag}/${archive_name}"
checksum_url="${archive_url}.sha256"
binaries="$(expected_binaries)"

if [ "$dry_run" -eq 1 ]; then
  log "gestalt installer dry run"
  log "component: ${component}"
  log "platform: ${platform}"
  log "tag: ${release_tag}"
  log "archive: ${archive_url}"
  log "checksum: ${checksum_url}"
  log "install directory: ${bin_dir}"
  log "binaries: ${binaries}"
  if [ "$user_install" -eq 1 ]; then
    warn_path
  fi
  exit 0
fi

archive_path="${tmp_dir}/${archive_name}"
checksum_path="${archive_path}.sha256"
extract_dir="${tmp_dir}/extract"

log "Downloading ${archive_url}"
download "$archive_url" "$archive_path" || fail "failed to download ${archive_url}"
download "$checksum_url" "$checksum_path" || fail "failed to download ${checksum_url}"
verify_checksum "$archive_path" "$archive_name" "$checksum_path"

mkdir -p "$extract_dir"
tar -xzf "$archive_path" -C "$extract_dir"

for binary in $binaries; do
  if [ ! -f "${extract_dir}/${binary}" ] || [ -L "${extract_dir}/${binary}" ]; then
    fail "${archive_name} did not contain expected regular binary: ${binary}"
  fi
done

ensure_install_dir

for binary in $binaries; do
  install_binary "${extract_dir}/${binary}" "$binary"
  log "Installed ${binary} to ${bin_dir}/${binary}"
done

if [ "$user_install" -eq 1 ]; then
  warn_path
fi

log "Done."
