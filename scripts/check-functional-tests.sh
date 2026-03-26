#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

violations=()

while IFS= read -r file; do
  [[ -z "$file" ]] && continue
  case "$file" in
    test/functional/*_test.go) ;;
    *) violations+=("$file") ;;
  esac
done < <(find . -type f -name '*_test.go' -print | sed 's|^\./||')

while IFS= read -r file; do
  [[ -z "$file" ]] && continue
  case "$file" in
    client/cli/tests/functional.rs) ;;
    *) violations+=("$file") ;;
  esac
done < <(find client/cli/tests -maxdepth 1 -type f -name '*.rs' -print | sed 's|^\./||')

while IFS= read -r file; do
  [[ -z "$file" ]] && continue
  case "$file" in
    web/e2e/*.spec.ts) ;;
    *) violations+=("$file") ;;
  esac
done < <(find . -type f \( -name '*.spec.ts' -o -name '*.test.ts' -o -name '*.spec.tsx' -o -name '*.test.tsx' -o -name '*.spec.js' -o -name '*.test.js' \) -print | sed 's|^\./||')

while IFS= read -r file; do
  [[ -z "$file" ]] && continue
  violations+=("$file (inline Rust #[cfg(test)] module)")
done < <(rg -l '#\[cfg\(test\)\]|mod tests' client/cli/src || true)

if ((${#violations[@]} > 0)); then
  printf 'Only functional tests are allowed in this repository.\n' >&2
  printf 'Unexpected test files or modules:\n' >&2
  printf '  %s\n' "${violations[@]}" >&2
  exit 1
fi
