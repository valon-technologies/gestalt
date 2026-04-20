#!/usr/bin/env bash

set -euo pipefail

if [[ -z "${GITHUB_REPOSITORY:-}" ]]; then
  printf 'GITHUB_REPOSITORY must be set\n' >&2
  exit 1
fi
if [[ -z "${APT_GPG_KEY_ID:-}" ]]; then
  printf 'APT_GPG_KEY_ID must be set\n' >&2
  exit 1
fi

site_dir=

while [[ $# -gt 0 ]]; do
  case "$1" in
    --site-dir)
      site_dir="$2"
      shift 2
      ;;
    *)
      printf 'unknown argument: %s\n' "$1" >&2
      exit 1
      ;;
  esac
done

if [[ -z "$site_dir" ]]; then
  printf 'usage: %s --site-dir DIR\n' "$0" >&2
  exit 1
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

repo_dir="${tmp_dir}/repo"
mkdir -p "${repo_dir}/conf" "${site_dir}/apt"

stable_suites=(jammy noble bookworm trixie)
preview_suites=(jammy-preview noble-preview bookworm-preview trixie-preview)

{
  for suite in "${stable_suites[@]}"; do
    cat <<EOF
Origin: Gestalt
Label: Gestalt
Suite: ${suite}
Codename: ${suite}
Architectures: amd64 arm64 armhf source
Components: main
Description: Gestalt APT repository (${suite})
SignWith: ${APT_GPG_KEY_ID}
Acquire-By-Hash: yes

EOF
  done

  for suite in "${preview_suites[@]}"; do
    cat <<EOF
Origin: Gestalt Preview
Label: Gestalt Preview
Suite: ${suite}
Codename: ${suite}
Architectures: amd64 arm64 armhf source
Components: main
Description: Gestalt APT preview repository (${suite})
SignWith: ${APT_GPG_KEY_ID}
Acquire-By-Hash: yes

EOF
  done
} > "${repo_dir}/conf/distributions"

gh api --paginate "repos/${GITHUB_REPOSITORY}/releases?per_page=100" | jq -s 'add' > "${tmp_dir}/releases.json"

jq -r '
  .[]
  | select(.draft | not)
  | select(.tag_name | test("^(gestalt|gestaltd)/v"))
  | {tag_name, prerelease, deb_assets: [.assets[] | select(.name | endswith(".deb")) | .name]}
  | select(.deb_assets | length > 0)
  | @base64
' "${tmp_dir}/releases.json" | while IFS= read -r encoded; do
  release_json="$(printf '%s' "${encoded}" | base64 --decode)"
  tag_name="$(printf '%s' "${release_json}" | jq -r '.tag_name')"
  prerelease="$(printf '%s' "${release_json}" | jq -r '.prerelease')"
  download_dir="${tmp_dir}/downloads/${tag_name//\//_}"
  mkdir -p "${download_dir}"
  gh release download "${tag_name}" --repo "${GITHUB_REPOSITORY}" --pattern "*.deb" --dir "${download_dir}"

  if [[ "${prerelease}" == "true" ]]; then
    suites=("${preview_suites[@]}")
  else
    suites=("${stable_suites[@]}")
  fi

  while IFS= read -r -d '' deb_file; do
    for suite in "${suites[@]}"; do
      reprepro -b "${repo_dir}" includedeb "${suite}" "${deb_file}"
    done
  done < <(find "${download_dir}" -type f -name '*.deb' -print0 | sort -z)
done

gpg --batch --yes --armor --export "${APT_GPG_KEY_ID}" > "${site_dir}/apt/gpg.asc"
gpg --batch --yes --export "${APT_GPG_KEY_ID}" > "${site_dir}/apt/gpg"

cat > "${site_dir}/index.html" <<'EOF'
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <title>Gestalt APT Repository</title>
  </head>
  <body>
    <h1>Gestalt APT Repository</h1>
    <p>This site publishes the Debian and Ubuntu apt repository for Gestalt.</p>
    <p>See the installation docs in the main repository for setup instructions.</p>
  </body>
</html>
EOF

cat > "${site_dir}/apt/index.html" <<'EOF'
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <title>Gestalt APT Repository</title>
  </head>
  <body>
    <h1>Gestalt APT Repository</h1>
    <p>Use this repository with Debian or Ubuntu apt clients.</p>
    <ul>
      <li>Stable suites: <code>jammy</code>, <code>noble</code>, <code>bookworm</code>, <code>trixie</code></li>
      <li>Preview suites: <code>jammy-preview</code>, <code>noble-preview</code>, <code>bookworm-preview</code>, <code>trixie-preview</code></li>
    </ul>
  </body>
</html>
EOF

cp -R "${repo_dir}/dists" "${site_dir}/apt/"
cp -R "${repo_dir}/pool" "${site_dir}/apt/"
