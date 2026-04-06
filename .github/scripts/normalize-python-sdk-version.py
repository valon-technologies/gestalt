#!/usr/bin/env python3

from __future__ import annotations

import argparse
import pathlib
import re


def to_pep440(version: str) -> str:
    match = re.fullmatch(r"(\d+\.\d+\.\d+)(?:-(alpha|beta|rc)\.(\d+))?", version)
    if match is None:
        raise SystemExit(f"unsupported Python SDK tag format: {version}")

    base, phase, phase_num = match.groups()
    if phase is None:
        return base
    return base + {"alpha": "a", "beta": "b", "rc": "rc"}[phase] + phase_num


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("pyproject")
    parser.add_argument("version")
    args = parser.parse_args()

    pep440 = to_pep440(args.version)
    path = pathlib.Path(args.pyproject)
    data = path.read_text()
    updated, count = re.subn(
        r'^version = "[^"]+"$',
        f'version = "{pep440}"',
        data,
        count=1,
        flags=re.MULTILINE,
    )
    if count != 1:
        raise SystemExit("failed to update [project].version in pyproject.toml")
    path.write_text(updated)
    print(f"normalized {args.version} -> {pep440}")


if __name__ == "__main__":
    main()
