#!/usr/bin/env bash
set -euo pipefail

repo_root=$(git rev-parse --show-toplevel)
module_dir=${1:-"$repo_root/gestaltd"}
base_ref=${2:-${PULL_REQUEST_BASE_SHA:-}}
head_ref=${3:-${PULL_REQUEST_HEAD_SHA:-HEAD}}

if [[ -z "$base_ref" ]]; then
	echo "./..."
	exit 0
fi

if ! git -C "$repo_root" cat-file -e "${base_ref}^{commit}" 2>/dev/null; then
	git -C "$repo_root" fetch --no-tags --depth=1 origin "$base_ref" >/dev/null 2>&1 || true
fi

if ! git -C "$repo_root" cat-file -e "${base_ref}^{commit}" 2>/dev/null; then
	echo "./..."
	exit 0
fi

if ! git -C "$repo_root" cat-file -e "${head_ref}^{commit}" 2>/dev/null; then
	head_ref=HEAD
fi

module_abs=$(cd "$module_dir" && pwd)
module_rel=${module_abs#"$repo_root"/}
if ! changed_files=$(git -C "$repo_root" diff --name-only "$base_ref" "$head_ref" -- "$module_rel"); then
	echo "./..."
	exit 0
fi

if [[ -z "$changed_files" ]]; then
	exit 0
fi

cd "$module_abs"

declare -a changed_packages=()

find_package_dir() {
	local path=$1
	local dir

	dir=$(dirname "$path")
	while [[ "$dir" != "." && "$dir" != "/" ]]; do
		if compgen -G "$dir/*.go" >/dev/null; then
			printf '%s\n' "$dir"
			return 0
		fi
		dir=$(dirname "$dir")
	done

	if compgen -G "./*.go" >/dev/null; then
		printf '.\n'
		return 0
	fi

	return 1
}

while IFS= read -r changed_file; do
	relative_path=${changed_file#"$module_rel"/}

	case "$relative_path" in
		go.mod|go.sum)
			echo "./..."
			exit 0
			;;
	esac

	if [[ ! -e "$relative_path" ]]; then
		case "$relative_path" in
			*.go)
				echo "./..."
				exit 0
				;;
			*)
				continue
				;;
		esac
	fi

	if ! package_dir=$(find_package_dir "$relative_path"); then
		continue
	fi

	if ! package_path=$(go list -f '{{.ImportPath}}' "./$package_dir" 2>/dev/null); then
		echo "./..."
		exit 0
	fi

	changed_packages+=("$package_path")
done <<< "$changed_files"

if [[ ${#changed_packages[@]} -eq 0 ]]; then
	exit 0
fi

changed_package_list=$(printf '%s\n' "${changed_packages[@]}" | sort -u)
printf '%s\n' "$changed_package_list"
