#!/usr/bin/env bash

set -euo pipefail

usage() {
  echo "usage: $0 <artifact> <current-tag> <output-file>" >&2
  exit 1
}

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

artifact=${1:-}
current_tag=${2:-}
output_file=${3:-}

[[ -n "$artifact" && -n "$current_tag" && -n "$output_file" ]] || usage

case "$artifact" in
  gestalt)
    pathspecs=("gestalt")
    ;;
  gestaltd)
    pathspecs=("gestaltd")
    ;;
  *)
    echo "unsupported artifact: $artifact" >&2
    exit 1
    ;;
esac

if ! git rev-parse --verify --quiet "refs/tags/$current_tag" >/dev/null; then
  echo "tag not found: $current_tag" >&2
  exit 1
fi

previous_tag=""
if previous_tag=$(git describe --tags --abbrev=0 --match "${artifact}/v*" "${current_tag}^" 2>/dev/null); then
  range="${previous_tag}..${current_tag}"
else
  previous_tag=""
  range="$current_tag"
fi

path_filter_label=$(join_by ', ' "${pathspecs[@]}")
commits=()
while IFS=$'\t' read -r sha subject; do
  [[ -n "$sha" ]] || continue
  commits+=("${sha}"$'\t'"${subject}")
done < <(git log --reverse --first-parent --format='%H%x09%s' "$range" -- "${pathspecs[@]}")

{
  printf '## Changes\n\n'

  if [[ -n "$previous_tag" ]]; then
    printf 'Scoped to `%s` changes since `%s`.\n\n' "$path_filter_label" "$previous_tag"
  else
    printf 'Scoped to `%s` changes in the first `%s` release.\n\n' "$path_filter_label" "$artifact"
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
