#!/usr/bin/env python3

from __future__ import annotations

import argparse
import os
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable
from urllib.parse import quote


CATEGORY_ORDER = (
    "New Features",
    "Fixes & Reliability",
    "Removals & Cleanup",
    "Improvements & Docs",
)


@dataclass(frozen=True)
class Commit:
    sha: str
    short_sha: str
    subject: str
    url: str | None
    category: str

    @property
    def markdown(self) -> str:
        if self.url:
            return f"- {self.subject} ([`{self.short_sha}`]({self.url}))"
        return f"- {self.subject} (`{self.short_sha}`)"


def run_git(*args: str, check: bool = True) -> str:
    result = subprocess.run(
        ["git", *args],
        check=check,
        capture_output=True,
        text=True,
    )
    return result.stdout.strip()


def split_lines(value: str) -> list[str]:
    return [line.strip() for line in value.splitlines() if line.strip()]


def replace_tokens(template: str, values: dict[str, str]) -> str:
    rendered = template
    for key, value in values.items():
        rendered = rendered.replace(f"${{{key}}}", value)
    return rendered


def normalize_subject(subject: str) -> str:
    normalized = subject.strip()
    while True:
        updated = re.sub(r"^\[[^\]]+\]\s*", "", normalized)
        if updated == normalized:
            break
        normalized = updated
    if normalized and normalized[0].islower():
        normalized = normalized[0].upper() + normalized[1:]
    return normalized


def categorize_subject(subject: str) -> str:
    lowered = re.sub(r"\s*\(#\d+\)$", "", subject).strip().lower()
    if re.search(r"\b(comment|comments|doc|docs|documentation|guide|readme)\b", lowered):
        return "Improvements & Docs"
    if re.match(
        r"^(add|allow|create|enable|expose|implement|introduce|render|ship|show|support)\b",
        lowered,
    ):
        return "New Features"
    if re.match(r"^improve\b", lowered):
        return "Improvements & Docs"
    if re.match(
        r"^(avoid|correct|fix|harden|protect|resolve|restore)\b",
        lowered,
    ):
        return "Fixes & Reliability"
    if re.match(r"^(delete|deprecate|drop|remove)\b", lowered):
        return "Removals & Cleanup"
    return "Improvements & Docs"


def path_label(paths: Iterable[str]) -> str:
    return ", ".join(f"`{path}`" for path in paths)


def compare_url(repo: str, previous_tag: str, current_tag: str) -> str:
    prev = quote(previous_tag, safe="")
    current = quote(current_tag, safe="")
    return f"https://github.com/{repo}/compare/{prev}...{current}"


def build_commit_list(
    revision_range: str,
    repo: str | None,
    path_filters: list[str],
    exclude_patterns: list[re.Pattern[str]],
) -> list[Commit]:
    output = run_git(
        "log",
        "--reverse",
        "--first-parent",
        "--format=%H%x09%s",
        revision_range,
        "--",
        *path_filters,
    )
    commits: list[Commit] = []
    for line in output.splitlines():
        if not line.strip():
            continue
        sha, raw_subject = line.split("\t", 1)
        if any(pattern.search(raw_subject) for pattern in exclude_patterns):
            continue
        subject = normalize_subject(raw_subject)
        url = f"https://github.com/{repo}/commit/{sha}" if repo else None
        commits.append(
            Commit(
                sha=sha,
                short_sha=sha[:7],
                subject=subject,
                url=url,
                category=categorize_subject(subject),
            )
        )
    return commits


def write_release_notes(
    output_path: Path,
    component_name: str,
    component_summary: str,
    install_markdown: str,
    current_tag: str,
    display_version: str,
    previous_tag: str | None,
    tag_glob: str,
    path_filters: list[str],
    commits: list[Commit],
    repo: str | None,
) -> None:
    token_values = {
        "component_name": component_name,
        "previous_tag": previous_tag or "",
        "repo": repo or "",
        "tag": current_tag,
        "version": display_version,
    }
    summary = replace_tokens(component_summary, token_values).strip()
    install = replace_tokens(install_markdown, token_values).strip()
    prerelease = bool(re.search(r"-(alpha|beta|rc)\b", display_version))

    lines: list[str] = [f"# {component_name} {display_version}", ""]

    if summary:
        lines.extend([summary, ""])

    if prerelease:
        lines.extend(
            [
                "> This is a pre-release build intended for early adopters and validation before a stable release.",
                "",
            ]
        )

    if install:
        lines.extend(["## Install / Upgrade", "", install, ""])

    lines.extend(["## Release Scope", ""])
    lines.append(f"- Release tag: `{current_tag}`")
    if previous_tag:
        lines.append(f"- Previous tag: `{previous_tag}`")
    else:
        lines.append(f"- Previous tag: first release matching `{tag_glob}`")
    if repo and previous_tag:
        lines.append(
            f"- Compare: [{previous_tag}...{current_tag}]({compare_url(repo, previous_tag, current_tag)})"
        )
    lines.append(f"- Included paths: {path_label(path_filters)}")
    lines.append(f"- Included commits: {len(commits)}")
    lines.append("")

    if not commits:
        lines.extend(
            [
                "## Changes",
                "",
                f"No commits matched the scoped paths for `{current_tag}`.",
                "",
            ]
        )
        output_path.write_text("\n".join(lines), encoding="utf-8")
        return

    lines.extend(["## Highlights", ""])
    for category in CATEGORY_ORDER:
        grouped = [commit for commit in commits if commit.category == category]
        if not grouped:
            continue
        lines.extend([f"### {category}", ""])
        lines.extend(commit.markdown for commit in grouped)
        lines.append("")

    lines.extend(["## Included Commits", ""])
    lines.extend(commit.markdown for commit in commits)
    lines.append("")

    if repo and previous_tag:
        lines.extend(
            [
                "## Full Compare",
                "",
                compare_url(repo, previous_tag, current_tag),
                "",
            ]
        )

    output_path.write_text("\n".join(lines), encoding="utf-8")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("tag_glob")
    parser.add_argument("current_tag")
    parser.add_argument("output_file")
    args = parser.parse_args()

    component_name = os.environ.get("COMPONENT_NAME", "").strip()
    version_prefix = os.environ.get("VERSION_PREFIX", "").strip()
    path_filters = split_lines(os.environ.get("PATH_FILTERS", ""))
    exclude_patterns = [
        re.compile(pattern, re.IGNORECASE)
        for pattern in split_lines(os.environ.get("EXCLUDE_SUBJECT_PATTERNS", ""))
    ]

    if not component_name or not version_prefix or not path_filters:
        print(
            "COMPONENT_NAME, VERSION_PREFIX, and PATH_FILTERS must be set",
            file=sys.stderr,
        )
        return 1

    try:
        run_git("rev-parse", "--verify", "--quiet", f"refs/tags/{args.current_tag}")
    except subprocess.CalledProcessError:
        print(f"tag not found: {args.current_tag}", file=sys.stderr)
        return 1

    previous_tag: str | None
    try:
        previous_tag = run_git(
            "describe",
            "--tags",
            "--abbrev=0",
            "--match",
            args.tag_glob,
            f"{args.current_tag}^",
        )
    except subprocess.CalledProcessError:
        previous_tag = None

    revision_range = (
        f"{previous_tag}..{args.current_tag}" if previous_tag else args.current_tag
    )
    repo = os.environ.get("GITHUB_REPOSITORY", "").strip() or None
    display_version = (
        args.current_tag[len(version_prefix) :]
        if args.current_tag.startswith(version_prefix)
        else args.current_tag
    )
    commits = build_commit_list(
        revision_range=revision_range,
        repo=repo,
        path_filters=path_filters,
        exclude_patterns=exclude_patterns,
    )
    write_release_notes(
        output_path=Path(args.output_file),
        component_name=component_name,
        component_summary=os.environ.get("COMPONENT_SUMMARY", ""),
        install_markdown=os.environ.get("INSTALL_MARKDOWN", ""),
        current_tag=args.current_tag,
        display_version=display_version,
        previous_tag=previous_tag,
        tag_glob=args.tag_glob,
        path_filters=path_filters,
        commits=commits,
        repo=repo,
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
