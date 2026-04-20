#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${script_dir}/lib.sh"

binary=
version=
arch=
out_dir=

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary)
      binary="$2"
      shift 2
      ;;
    --version)
      version="$2"
      shift 2
      ;;
    --arch)
      arch="$2"
      shift 2
      ;;
    --out-dir)
      out_dir="$2"
      shift 2
      ;;
    *)
      printf 'unknown argument: %s\n' "$1" >&2
      exit 1
      ;;
  esac
done

if [[ -z "$binary" || -z "$version" || -z "$arch" || -z "$out_dir" ]]; then
  printf 'usage: %s --binary PATH --version VERSION --arch ARCH --out-dir DIR\n' "$0" >&2
  exit 1
fi

pkg_arch="$(debian_arch "$arch")"
pkg_version="$(debian_version "$version")"
pkg_name="gestalt"
stage_dir="$(mktemp -d)"
trap 'rm -rf "${stage_dir}"' EXIT

mkdir -p "${stage_dir}/DEBIAN" "${stage_dir}/usr/bin"
install -m 0755 "$binary" "${stage_dir}/usr/bin/gestalt"

cat > "${stage_dir}/DEBIAN/control" <<EOF
Package: ${pkg_name}
Version: ${pkg_version}
Section: utils
Priority: optional
Architecture: ${pkg_arch}
Maintainer: ${GESTALT_PACKAGE_MAINTAINER}
Depends: ca-certificates
Homepage: ${GESTALT_PACKAGE_HOMEPAGE}
Description: Gestalt CLI
 Command-line client for authenticating with Gestalt, connecting plugins,
 invoking operations, and managing API tokens from the terminal.
EOF

mkdir -p "$out_dir"
pkg_file="${out_dir}/${pkg_name}_${pkg_version}_${pkg_arch}.deb"
dpkg-deb --build --root-owner-group "${stage_dir}" "${pkg_file}"
write_sha256_file "${pkg_file}"
