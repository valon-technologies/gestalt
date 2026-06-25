#!/usr/bin/env python3
"""Migrate provider manifests for plan-17 unified install/build/run semantics."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

try:
    import tomllib
except ImportError:  # pragma: no cover
    import tomli as tomllib  # type: ignore

GO_BLOCK = """install:
  command: [go, mod, download]
build:
  command: [go, run, github.com/valon-technologies/gestalt/sdk/go/cmd/gestalt, build]
run:
  command: [go, run, github.com/valon-technologies/gestalt/sdk/go/cmd/gestalt, run]
"""

GO_PROVIDER_PATHS = [
    "auth/oidc",
    "authorization/indexeddb",
    "cache/valkey",
    "externalcredentials/default",
    "indexeddb/relationaldb",
    "runtime/gkeagentsandbox",
    "s3/gcs",
    "s3/s3",
    "secrets/google",
    "workflow/indexeddb",
    "workflow/temporal",
]


def read_text(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def write_text(path: Path, text: str) -> None:
    path.write_text(text, encoding="utf-8")


def replace_command_tokens(text: str) -> str:
    text = text.replace("gestalt-ts-build", "gestalt, build")
    text = text.replace("gestalt-ts-runtime", "gestalt, run")
    text = re.sub(
        r"\[uv, run, python, -m, gestalt\._build\]",
        "[uv, run, gestalt, build]",
        text,
    )
    text = re.sub(
        r"\[uv, run, python, -m, gestalt\._runtime[^\]]*\]",
        "[uv, run, gestalt, run, \".\", \"provider:provider\"]",
        text,
    )
    return text


def rename_dev_to_run(text: str) -> str:
    return re.sub(r"(?m)^dev:", "run:", text)


def ts_run_command(manifest_dir: Path) -> list[str] | None:
    package_json = manifest_dir / "package.json"
    if not package_json.exists():
        return None
    data = json.loads(read_text(package_json))
    target = data.get("gestalt", {}).get("provider", {}).get("target")
    if not target:
        return None
    return ["bun", "run", "gestalt", "run", ".", target]


def python_run_command(manifest_dir: Path) -> list[str] | None:
    pyproject = manifest_dir / "pyproject.toml"
    if not pyproject.exists():
        return None
    data = tomllib.loads(read_text(pyproject))
    provider = data.get("tool", {}).get("gestalt", {}).get("provider")
    if not provider:
        return None
    return ["uv", "run", "gestalt", "run", ".", provider]


def format_run_block(command: list[str]) -> str:
  inner = ", ".join(command)
  return f"run:\n  command: [{inner}]\n"


def ensure_run_block(text: str, manifest_dir: Path, kind: str) -> str:
    if re.search(r"(?m)^run:", text):
        return text
    if kind == "ui":
        return text
    command = None
    if (manifest_dir / "package.json").exists():
        command = ts_run_command(manifest_dir)
    elif (manifest_dir / "pyproject.toml").exists():
        command = python_run_command(manifest_dir)
    if command is None:
        return text
    block = format_run_block(command)
    lines = text.splitlines(keepends=True)
    insert_at = len(lines)
    for i, line in enumerate(lines):
        if line.startswith("spec:"):
            insert_at = i
            break
    lines.insert(insert_at, block)
    return "".join(lines)


def insert_go_blocks(text: str) -> str:
    if "build:" in text or "run:" in text or "install:" in text:
        return text
    if re.search(r"(?m)^spec:", text):
        return re.sub(r"(?m)^spec:", GO_BLOCK + "spec:", text, count=1)
    return text + "\n" + GO_BLOCK


def migrate_manifest(path: Path) -> bool:
    original = read_text(path)
    text = original
    text = replace_command_tokens(text)
    kind_match = re.search(r"(?m)^kind:\s*(\S+)", text)
    kind = kind_match.group(1) if kind_match else ""
    if kind == "ui":
        text = rename_dev_to_run(text)
    else:
        text = ensure_run_block(text, path.parent, kind)
    changed = text != original
    if changed:
        write_text(path, text)
    return changed


def migrate_go_providers(repo: Path) -> int:
    count = 0
    for rel in GO_PROVIDER_PATHS:
        manifest = repo / rel / "manifest.yaml"
        if not manifest.exists():
            print(f"skip missing {manifest}")
            continue
        original = read_text(manifest)
        text = insert_go_blocks(original)
        if text != original:
            write_text(manifest, text)
            count += 1
            print(f"updated go provider {rel}")
    return count


def migrate_tree(root: Path) -> int:
    count = 0
    for manifest in sorted(root.rglob("manifest.yaml")):
        if migrate_manifest(manifest):
            count += 1
            print(f"updated {manifest.relative_to(root)}")
    return count


def main(argv: list[str]) -> int:
    if len(argv) < 2:
        print("usage: migrate-plan17-manifests.py <repo-root> [--go-only]", file=sys.stderr)
        return 2
    repo = Path(argv[1])
    if not repo.is_dir():
        print(f"not a directory: {repo}", file=sys.stderr)
        return 1
    total = 0
    if "--go-only" in argv:
        total += migrate_go_providers(repo)
    else:
        total += migrate_tree(repo)
        if repo.name == "gestalt-providers":
            total += migrate_go_providers(repo)
    print(f"done: {total} manifest(s) updated")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
