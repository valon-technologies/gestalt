#!/usr/bin/env bash

set -euo pipefail

usage() {
  echo "usage: $0 <tag-glob> <current-tag> <output-file> <path-filter> [<path-filter> ...]" >&2
  exit 1
}

tag_glob=${1:-}
current_tag=${2:-}
output_file=${3:-}
shift 3
path_filters=("$@")

[[ -n "$tag_glob" && -n "$current_tag" && -n "$output_file" && ${#path_filters[@]} -gt 0 ]] || usage

join_by() {
  local separator=$1
  shift
  local first=1
  for value in "$@"; do
    if [[ $first -eq 1 ]]; then
      printf '%s' "$value"
      first=0
    else
      printf '%s%s' "$separator" "$value"
    fi
  done
}

if ! git rev-parse --verify --quiet "refs/tags/$current_tag" >/dev/null; then
  echo "tag not found: $current_tag" >&2
  exit 1
fi

previous_tag=""
if previous_tag=$(git describe --tags --abbrev=0 --match "$tag_glob" "${current_tag}^" 2>/dev/null); then
  range="${previous_tag}..${current_tag}"
else
  previous_tag=""
  range="$current_tag"
fi

commits=()
while IFS=$'\t' read -r sha subject; do
  [[ -n "$sha" ]] || continue
  commits+=("${sha}"$'\t'"${subject}")
done < <(git log --reverse --first-parent --format='%H%x09%s' "$range" -- "${path_filters[@]}")

path_filter_label=$(join_by ', ' "${path_filters[@]}")

{
  printf '## Changes\n\n'

  if [[ -n "$previous_tag" ]]; then
    printf 'Scoped to `%s` changes since `%s`.\n\n' "$path_filter_label" "$previous_tag"
  else
    printf 'Scoped to `%s` changes in the first release matching `%s`.\n\n' "$path_filter_label" "$tag_glob"
  fi

  if [[ ${#commits[@]} -eq 0 ]]; then
    printf -- '- No commits matched the `%s` path filter in this release range.\n' "$path_filter_label"
  else
    for entry in "${commits[@]}"; do
      sha=${entry%%$'\t'*}
      subject=${entry#*$'\t'}
      short_sha=${sha:0:7}
      if [[ -n "${GITHUB_REPOSITORY:-}" ]]; then
        printf -- '- %s ([`%s`](https://github.com/%s/commit/%s))\n' "$subject" "$short_sha" "$GITHUB_REPOSITORY" "$sha"
      else
        printf -- '- %s (`%s`)\n' "$subject" "$short_sha"
      fi
    done
  fi
} >"$output_file"
