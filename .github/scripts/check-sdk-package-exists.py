#!/usr/bin/env python3

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Check whether an SDK package version exists.")
    parser.add_argument("registry", choices=["pypi", "npm"])
    parser.add_argument("package")
    parser.add_argument("version")
    parser.add_argument(
        "--metadata-file",
        type=Path,
        help="Read registry metadata from this file instead of the network.",
    )
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    metadata = read_metadata(args.registry, args.package, args.metadata_file)
    exists = version_exists(args.registry, metadata, args.version)
    value = "true" if exists else "false"
    github_output = os.environ.get("GITHUB_OUTPUT")
    if github_output:
        with open(github_output, "a", encoding="utf-8") as output:
            output.write(f"exists={value}\n")
    print(f"exists={value}")


def read_metadata(registry: str, package: str, metadata_file: Path | None) -> dict:
    if metadata_file:
        return json.loads(metadata_file.read_text())
    url = registry_url(registry, package)
    request = urllib.request.Request(url, headers={"Accept": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        if error.code == 404:
            return {}
        raise


def registry_url(registry: str, package: str) -> str:
    if registry == "pypi":
        return f"https://pypi.org/pypi/{urllib.parse.quote(package)}/json"
    return "https://registry.npmjs.org/" + urllib.parse.quote(package, safe="@")


def version_exists(registry: str, metadata: dict, version: str) -> bool:
    if registry == "pypi":
        return version in metadata.get("releases", {})
    return version in metadata.get("versions", {})


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        sys.exit(130)
