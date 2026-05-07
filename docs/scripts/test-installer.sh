#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

installer="$PWD/public/install.sh"
cli_installer="$PWD/public/install-gestalt.sh"
daemon_installer="$PWD/public/install-gestaltd.sh"
tmp_dir="$(mktemp -d)"
download_root="$tmp_dir/releases"

cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

fail() {
  printf 'test-installer: %s\n' "$*" >&2
  exit 1
}

assert_contains() {
  local text="$1"
  local expected="$2"
  case "$text" in
    *"$expected"*) ;;
    *) fail "expected output to contain: $expected"$'\n'"actual output:"$'\n'"$text" ;;
  esac
}

assert_executable() {
  local path="$1"
  [[ -x "$path" ]] || fail "expected executable: $path"
}

assert_not_exists() {
  local path="$1"
  [[ ! -e "$path" ]] || fail "expected path not to exist: $path"
}

assert_fails() {
  local output
  local status
  set +e
  output="$("$@" 2>&1)"
  status=$?
  set -e
  if [[ "$status" -eq 0 ]]; then
    fail "command unexpectedly succeeded: $*"
  fi
  printf '%s' "$output"
}

checksum_archive() {
  local archive="$1"
  local archive_name
  archive_name="$(basename "$archive")"

  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$(dirname "$archive")" && sha256sum "$archive_name")
  else
    (cd "$(dirname "$archive")" && shasum -a 256 "$archive_name")
  fi
}

make_archive() {
  local family="$1"
  local version="$2"
  local platform="$3"
  shift 3

  local stage_dir="$tmp_dir/stage/${family}-${version}-${platform}"
  local dest_dir="$download_root/${family}/v${version}"
  local archive="${dest_dir}/${family}-${platform}.tar.gz"

  mkdir -p "$stage_dir" "$dest_dir"
  for binary in "$@"; do
    {
      printf '#!/bin/sh\n'
      printf 'printf "%s %s\\n"\n' "$binary" "$version"
    } >"${stage_dir}/${binary}"
    chmod +x "${stage_dir}/${binary}"
  done

  (cd "$stage_dir" && tar -czf "$archive" "$@")
  checksum_archive "$archive" >"${archive}.sha256"
}

run_core_installer() {
  env \
    GESTALT_INSTALL_DOWNLOAD_BASE="file://${download_root}" \
    GESTALT_INSTALL_OS=linux \
    GESTALT_INSTALL_ARCH=x86_64 \
    sh "$installer" "$@"
}

run_cli_installer() {
  env \
    GESTALT_INSTALL_SCRIPT_URL="file://${installer}" \
    GESTALT_INSTALL_DOWNLOAD_BASE="file://${download_root}" \
    GESTALT_INSTALL_OS=linux \
    GESTALT_INSTALL_ARCH=x86_64 \
    sh "$cli_installer" "$@"
}

run_daemon_installer() {
  env \
    GESTALT_INSTALL_SCRIPT_URL="file://${installer}" \
    GESTALT_INSTALL_DOWNLOAD_BASE="file://${download_root}" \
    GESTALT_INSTALL_OS=linux \
    GESTALT_INSTALL_ARCH=x86_64 \
    sh "$daemon_installer" "$@"
}

make_archive gestalt 1.2.3 linux-x86_64 gestalt
make_archive gestaltd 2.0.0 linux-x86_64 gestaltd
make_archive gestaltd 3.0.0 linux-x86_64 gestalt
make_archive gestalt 4.0.0 linux-x86_64 gestalt
make_archive gestalt 5.0.0 linux-x86_64 gestalt
make_archive gestalt 6.0.0 linux-x86_64 gestalt
make_archive gestalt 8.0.0 macos-arm64 gestalt

symlink_stage="$tmp_dir/stage/gestalt-7.0.0-linux-x86_64"
symlink_dest="$download_root/gestalt/v7.0.0"
mkdir -p "$symlink_stage" "$symlink_dest"
ln -s /etc/hosts "$symlink_stage/gestalt"
(cd "$symlink_stage" && tar -czf "$symlink_dest/gestalt-linux-x86_64.tar.gz" gestalt)
checksum_archive "$symlink_dest/gestalt-linux-x86_64.tar.gz" >"$symlink_dest/gestalt-linux-x86_64.tar.gz.sha256"

cli_bin="$tmp_dir/bin-cli"
run_cli_installer --version gestalt/v1.2.3 --bin-dir "$cli_bin" >/dev/null
assert_executable "$cli_bin/gestalt"
assert_not_exists "$cli_bin/gestaltd"

daemon_bin="$tmp_dir/bin-daemon"
run_daemon_installer --version 2.0.0 --bin-dir "$daemon_bin" >/dev/null
assert_executable "$daemon_bin/gestaltd"
assert_not_exists "$daemon_bin/gestalt"

default_bin="$tmp_dir/bin-default"
run_core_installer --version 1.2.3 --bin-dir "$default_bin" >/dev/null
assert_executable "$default_bin/gestalt"
assert_not_exists "$default_bin/gestaltd"

macos_bin="$tmp_dir/bin-macos"
env \
  GESTALT_INSTALL_DOWNLOAD_BASE="file://${download_root}" \
  GESTALT_INSTALL_OS=Darwin \
  GESTALT_INSTALL_ARCH=arm64 \
  sh "$installer" --component gestalt --version 8.0.0 --bin-dir "$macos_bin" >/dev/null
assert_executable "$macos_bin/gestalt"
assert_not_exists "$macos_bin/gestaltd"

cat >"$tmp_dir/releases-page-1.json" <<'JSON'
[
  {
    "tag_name": "sdk/go/v9.9.9",
    "draft": false,
    "prerelease": true
  },
  {
    "tag_name": "gestalt/v9.9.9",
    "draft": true,
    "prerelease": true
  }
]
JSON

cat >"$tmp_dir/releases-page-2.json" <<'JSON'
[
  {
    "tag_name": "gestalt/v0.2.0-alpha.1",
    "draft": false,
    "prerelease": true
  },
  {
    "tag_name": "gestaltd/v0.9.0-alpha.1",
    "draft": false,
    "prerelease": true
  }
]
JSON

latest_cli_output="$(
  env \
    GESTALT_INSTALL_SCRIPT_URL="file://${installer}" \
    GESTALT_INSTALL_RELEASES_URL="file://${tmp_dir}/releases-page-{page}.json" \
    GESTALT_INSTALL_DOWNLOAD_BASE="file:///does-not-exist" \
    GESTALT_INSTALL_MAX_PAGES=2 \
    GESTALT_INSTALL_OS=linux \
    GESTALT_INSTALL_ARCH=aarch64 \
    sh "$cli_installer" --dry-run --bin-dir "$tmp_dir/dry-run-bin"
)"
assert_contains "$latest_cli_output" "component: gestalt"
assert_contains "$latest_cli_output" "tag: gestalt/v0.2.0-alpha.1"
assert_contains "$latest_cli_output" "gestalt-linux-arm64.tar.gz"
assert_contains "$latest_cli_output" "binaries: gestalt"

latest_daemon_output="$(
  env \
    GESTALT_INSTALL_SCRIPT_URL="file://${installer}" \
    GESTALT_INSTALL_RELEASES_URL="file://${tmp_dir}/releases-page-{page}.json" \
    GESTALT_INSTALL_DOWNLOAD_BASE="file:///does-not-exist" \
    GESTALT_INSTALL_MAX_PAGES=2 \
    GESTALT_INSTALL_OS=linux \
    GESTALT_INSTALL_ARCH=armv7l \
    sh "$daemon_installer" --dry-run --bin-dir "$tmp_dir/dry-run-bin"
)"
assert_contains "$latest_daemon_output" "component: gestaltd"
assert_contains "$latest_daemon_output" "tag: gestaltd/v0.9.0-alpha.1"
assert_contains "$latest_daemon_output" "gestaltd-linux-armv7.tar.gz"
assert_contains "$latest_daemon_output" "binaries: gestaltd"

dry_run_dead_base="$(
  env \
    GESTALT_INSTALL_SCRIPT_URL="file://${installer}" \
    GESTALT_INSTALL_DOWNLOAD_BASE="file:///does-not-exist" \
    GESTALT_INSTALL_OS=linux \
    GESTALT_INSTALL_ARCH=x86_64 \
    sh "$cli_installer" --version 1.2.3 --dry-run --bin-dir "$tmp_dir/dry-run-bin"
)"
assert_contains "$dry_run_dead_base" "archive: file:///does-not-exist/gestalt/v1.2.3/gestalt-linux-x86_64.tar.gz"

dry_run_macos="$(
  env \
    GESTALT_INSTALL_SCRIPT_URL="file://${installer}" \
    GESTALT_INSTALL_DOWNLOAD_BASE="file:///does-not-exist" \
    GESTALT_INSTALL_OS=darwin \
    GESTALT_INSTALL_ARCH=arm64 \
    sh "$cli_installer" --version 8.0.0 --dry-run --bin-dir "$tmp_dir/dry-run-bin"
)"
assert_contains "$dry_run_macos" "platform: macos-arm64"
assert_contains "$dry_run_macos" "archive: file:///does-not-exist/gestalt/v8.0.0/gestalt-macos-arm64.tar.gz"

unsupported_macos_output="$(
  assert_fails env \
    GESTALT_INSTALL_OS=darwin \
    GESTALT_INSTALL_ARCH=armv7l \
    sh "$installer" --component gestalt --dry-run --bin-dir "$tmp_dir/unsupported-bin"
)"
assert_contains "$unsupported_macos_output" "unsupported darwin architecture: armv7l"

mismatch_output="$(
  assert_fails env \
    GESTALT_INSTALL_OS=linux \
    GESTALT_INSTALL_ARCH=x86_64 \
    sh "$installer" --component gestaltd --version gestalt/v1.2.3 --dry-run --bin-dir "$tmp_dir/mismatch-bin"
)"
assert_contains "$mismatch_output" "does not match --component gestaltd"

both_output="$(assert_fails sh "$installer" --component both --dry-run)"
assert_contains "$both_output" "--component both is no longer supported"

wrapper_component_output="$(assert_fails sh "$cli_installer" --component gestaltd --dry-run)"
assert_contains "$wrapper_component_output" "install-gestalt.sh does not accept --component"

cli_help_output="$(sh "$cli_installer" --help)"
assert_contains "$cli_help_output" "Install the Gestalt CLI on Linux or macOS."

daemon_help_output="$(sh "$daemon_installer" --help)"
assert_contains "$daemon_help_output" "Install gestaltd on Linux or macOS."

conflict_output="$(assert_fails sh "$installer" --user --bin-dir "$tmp_dir/conflict-bin")"
assert_contains "$conflict_output" "mutually exclusive"

missing_output="$(
  assert_fails env \
    GESTALT_INSTALL_DOWNLOAD_BASE="file://${download_root}" \
    GESTALT_INSTALL_OS=linux \
    GESTALT_INSTALL_ARCH=x86_64 \
    sh "$installer" --component gestaltd --version 3.0.0 --bin-dir "$tmp_dir/missing-bin"
)"
assert_contains "$missing_output" "did not contain expected regular binary: gestaltd"

symlink_output="$(
  assert_fails env \
    GESTALT_INSTALL_DOWNLOAD_BASE="file://${download_root}" \
    GESTALT_INSTALL_OS=linux \
    GESTALT_INSTALL_ARCH=x86_64 \
    sh "$installer" --component gestalt --version 7.0.0 --bin-dir "$tmp_dir/symlink-bin"
)"
assert_contains "$symlink_output" "did not contain expected regular binary: gestalt"

bad_checksum="$download_root/gestalt/v4.0.0/gestalt-linux-x86_64.tar.gz.sha256"
printf '0000000000000000000000000000000000000000000000000000000000000000  gestalt-linux-x86_64.tar.gz\n' >"$bad_checksum"
checksum_output="$(
  assert_fails env \
    GESTALT_INSTALL_DOWNLOAD_BASE="file://${download_root}" \
    GESTALT_INSTALL_OS=linux \
    GESTALT_INSTALL_ARCH=x86_64 \
    sh "$installer" --component gestalt --version 4.0.0 --bin-dir "$tmp_dir/bad-checksum-bin"
)"
assert_contains "$checksum_output" "checksum mismatch"

empty_checksum="$download_root/gestalt/v5.0.0/gestalt-linux-x86_64.tar.gz.sha256"
: >"$empty_checksum"
malformed_output="$(
  assert_fails env \
    GESTALT_INSTALL_DOWNLOAD_BASE="file://${download_root}" \
    GESTALT_INSTALL_OS=linux \
    GESTALT_INSTALL_ARCH=x86_64 \
    sh "$installer" --component gestalt --version 5.0.0 --bin-dir "$tmp_dir/empty-checksum-bin"
)"
assert_contains "$malformed_output" "checksum file is malformed"

sha_tool_output="$(
  assert_fails env \
    GESTALT_INSTALL_DOWNLOAD_BASE="file://${download_root}" \
    GESTALT_INSTALL_OS=linux \
    GESTALT_INSTALL_ARCH=x86_64 \
    GESTALT_INSTALL_SHA256_TOOL=none \
    sh "$installer" --component gestalt --version 6.0.0 --bin-dir "$tmp_dir/no-sha-bin"
)"
assert_contains "$sha_tool_output" "no supported SHA-256 tool"

if [[ -d out ]]; then
  for script in install.sh install-gestalt.sh install-gestaltd.sh; do
    [[ -f "out/${script}" ]] || fail "docs static export did not include out/${script}"
    first_line="$(sed -n '1p' "out/${script}")"
    [[ "$first_line" == "#!/bin/sh" ]] || fail "out/${script} did not preserve installer shebang"
  done
else
  printf 'Skipping static export assertion because docs/out does not exist; run npm run build first.\n'
fi

printf 'Installer integration tests passed.\n'
