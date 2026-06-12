#!/usr/bin/env python3

from __future__ import annotations

import argparse
import base64
import dataclasses
import datetime as dt
import hashlib
import json
import mimetypes
import os
import re
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path


VERSION_CACHE_CONTROL = "public, max-age=31536000, immutable"
LATEST_CACHE_CONTROL = "public, max-age=60, must-revalidate"
REDIRECT_CACHE_CONTROL = "no-cache"
MARKER_CACHE_CONTROL = "no-cache"


@dataclasses.dataclass(frozen=True)
class ObjectSpec:
    key: str
    cache_control: str
    content_type: str
    source: str
    content: bytes
    immutable: bool

    def manifest_entry(self) -> dict[str, object]:
        return {
            "key": self.key,
            "cacheControl": self.cache_control,
            "contentType": self.content_type,
            "source": self.source,
            "immutable": self.immutable,
            "sha256ContentBase64": base64.b64encode(hashlib.sha256(self.content).digest()).decode(
                "ascii"
            ),
            "bytes": len(self.content),
        }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Publish versioned SDK API docs to the Gestalt docs bucket.",
    )
    parser.add_argument("--language", choices=["python", "typescript"], required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--source-dir", type=Path, required=True)
    parser.add_argument("--bucket")
    parser.add_argument("--source-tag", required=True)
    parser.add_argument(
        "--latest-policy",
        choices=["auto", "force", "never"],
        default="auto",
        help="Whether this release may update the moving latest alias.",
    )
    parser.add_argument(
        "--mode",
        choices=["manifest", "upload"],
        default="upload",
        help="manifest prints the upload plan without contacting GCS.",
    )
    parser.add_argument(
        "--current-latest-version",
        help="Testing override for the latest marker comparison in manifest mode.",
    )
    parser.add_argument(
        "--force-version-overwrite",
        action="store_true",
        help="Allow replacing existing immutable version objects with different content.",
    )
    parser.add_argument(
        "--generated-at",
        help="Testing override for latest.json generatedAt.",
    )
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    validate_version(args.language, args.version)
    source_dir = args.source_dir.resolve()
    if not source_dir.is_dir():
        raise SystemExit(f"source dir does not exist: {source_dir}")

    current_latest = args.current_latest_version
    if args.mode == "upload":
        if not args.bucket:
            raise SystemExit("--bucket is required in upload mode")
        current_latest = read_current_latest(args.bucket, args.language)

    update_latest = should_update_latest(
        language=args.language,
        candidate=args.version,
        current=current_latest,
        latest_policy=args.latest_policy,
    )
    objects = build_objects(
        language=args.language,
        version=args.version,
        source_tag=args.source_tag,
        source_dir=source_dir,
        update_latest=update_latest,
        generated_at=args.generated_at,
    )

    manifest = {
        "language": args.language,
        "version": args.version,
        "sourceTag": args.source_tag,
        "latestPolicy": args.latest_policy,
        "currentLatestVersion": current_latest,
        "updatesLatest": update_latest,
        "cleanupLatestPrefix": f"api/{args.language}/latest/" if update_latest else None,
        "objects": [obj.manifest_entry() for obj in objects],
    }

    if args.mode == "manifest":
        print(json.dumps(manifest, indent=2, sort_keys=True))
        return

    token = gcloud_access_token()
    for obj in objects:
        upload_object(
            bucket=args.bucket,
            spec=obj,
            token=token,
            force_version_overwrite=args.force_version_overwrite,
        )
    if update_latest:
        cleanup_latest_prefix(
            bucket=args.bucket,
            language=args.language,
            keep_keys={obj.key for obj in objects},
            token=token,
        )
    print(json.dumps({"uploaded": len(objects), "updatesLatest": update_latest}, sort_keys=True))


def validate_version(language: str, version: str) -> None:
    parse_version(language, version)


def parse_version(language: str, version: str) -> tuple[int, int, int, int, int]:
    if language == "python":
        match = re.fullmatch(r"(\d+)\.(\d+)\.(\d+)(?:(a|b|rc)(\d+))?", version)
        rank = {"a": 0, "b": 1, "rc": 2, None: 3}
    else:
        match = re.fullmatch(r"(\d+)\.(\d+)\.(\d+)(?:-(alpha|beta|rc)\.(\d+))?", version)
        rank = {"alpha": 0, "beta": 1, "rc": 2, None: 3}
    if match is None:
        raise SystemExit(f"unsupported {language} SDK docs version: {version}")
    major, minor, patch, phase, phase_num = match.groups()
    return (
        int(major),
        int(minor),
        int(patch),
        rank[phase],
        int(phase_num or 0),
    )


def should_update_latest(
    *,
    language: str,
    candidate: str,
    current: str | None,
    latest_policy: str,
) -> bool:
    if latest_policy == "force":
        return True
    if latest_policy == "never":
        return False
    if not current:
        return True
    return parse_version(language, candidate) >= parse_version(language, current)


def build_objects(
    *,
    language: str,
    version: str,
    source_tag: str,
    source_dir: Path,
    update_latest: bool,
    generated_at: str | None,
) -> list[ObjectSpec]:
    objects: list[ObjectSpec] = []
    version_prefix = f"api/{language}/{version}/"
    latest_prefix = f"api/{language}/latest/"

    files = sorted(path for path in source_dir.rglob("*") if path.is_file())
    if not any(path.relative_to(source_dir).as_posix() == "index.html" for path in files):
        raise SystemExit(f"SDK docs root is missing index.html: {source_dir}")

    for path in files:
        rel = path.relative_to(source_dir).as_posix()
        content = path.read_bytes()
        content_type = guess_content_type(path)
        objects.append(
            ObjectSpec(
                key=version_prefix + rel,
                cache_control=VERSION_CACHE_CONTROL,
                content_type=content_type,
                source=str(path),
                content=content,
                immutable=True,
            )
        )
        if rel == "index.html":
            objects.append(
                ObjectSpec(
                    key=version_prefix,
                    cache_control=VERSION_CACHE_CONTROL,
                    content_type=content_type,
                    source=str(path),
                    content=content,
                    immutable=True,
                )
            )
        if update_latest:
            objects.append(
                ObjectSpec(
                    key=latest_prefix + rel,
                    cache_control=LATEST_CACHE_CONTROL,
                    content_type=content_type,
                    source=str(path),
                    content=content,
                    immutable=False,
                )
            )
            if rel == "index.html":
                objects.append(
                    ObjectSpec(
                        key=latest_prefix,
                        cache_control=LATEST_CACHE_CONTROL,
                        content_type=content_type,
                        source=str(path),
                        content=content,
                        immutable=False,
                    )
                )

    if update_latest:
        redirect = redirect_html(f"/api/{language}/latest/")
        for key in [f"api/{language}", f"api/{language}/", f"api/{language}/index.html"]:
            objects.append(
                ObjectSpec(
                    key=key,
                    cache_control=REDIRECT_CACHE_CONTROL,
                    content_type="text/html; charset=utf-8",
                    source="generated legacy redirect",
                    content=redirect,
                    immutable=False,
                )
            )
        marker = {
            "language": language,
            "version": version,
            "sourceTag": source_tag,
            "generatedAt": generated_at or dt.datetime.now(dt.UTC).isoformat(),
        }
        objects.append(
            ObjectSpec(
                key=f"api/{language}/latest.json",
                cache_control=MARKER_CACHE_CONTROL,
                content_type="application/json; charset=utf-8",
                source="generated latest marker",
                content=(json.dumps(marker, indent=2, sort_keys=True) + "\n").encode("utf-8"),
                immutable=False,
            )
        )

    keys = [obj.key for obj in objects]
    if len(keys) != len(set(keys)):
        duplicates = sorted({key for key in keys if keys.count(key) > 1})
        raise SystemExit(f"duplicate object keys in SDK docs manifest: {duplicates}")
    return objects


def guess_content_type(path: Path) -> str:
    content_type, _ = mimetypes.guess_type(path.name)
    if path.suffix == ".js":
        return "text/javascript"
    return content_type or "application/octet-stream"


def redirect_html(target: str) -> bytes:
    return f"""<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta http-equiv="refresh" content="0; url={target}">
    <link rel="canonical" href="{target}">
    <script>location.replace({json.dumps(target)});</script>
    <title>Redirecting</title>
  </head>
  <body><a href="{target}">Redirecting to {target}</a></body>
</html>
""".encode("utf-8")


def read_current_latest(bucket: str, language: str) -> str | None:
    token = gcloud_access_token()
    key = f"api/{language}/latest.json"
    try:
        data = download_object(bucket, key, token)
    except FileNotFoundError:
        return None
    marker = json.loads(data.decode("utf-8"))
    return marker.get("version")


def gcloud_access_token() -> str:
    token = os.environ.get("GOOGLE_OAUTH_ACCESS_TOKEN")
    if token:
        return token
    try:
        result = subprocess.run(
            ["gcloud", "auth", "print-access-token"],
            check=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
    except (OSError, subprocess.CalledProcessError) as error:
        raise SystemExit(f"failed to get gcloud access token: {error}") from error
    return result.stdout.strip()


def upload_object(
    *,
    bucket: str,
    spec: ObjectSpec,
    token: str,
    force_version_overwrite: bool,
) -> None:
    existing = None
    try:
        existing = download_object(bucket, spec.key, token)
    except FileNotFoundError:
        pass

    if spec.immutable and existing is not None:
        if existing == spec.content:
            print(f"skip identical immutable object gs://{bucket}/{spec.key}")
            return
        if not force_version_overwrite:
            raise SystemExit(
                f"immutable SDK docs object already exists with different content: "
                f"gs://{bucket}/{spec.key}"
            )

    metadata = {
        "name": spec.key,
        "cacheControl": spec.cache_control,
        "contentType": spec.content_type,
    }
    multipart_upload(bucket, metadata, spec.content, token)
    print(f"uploaded gs://{bucket}/{spec.key}")


def download_object(bucket: str, key: str, token: str) -> bytes:
    encoded_key = urllib.parse.quote(key, safe="")
    request = urllib.request.Request(
        f"https://storage.googleapis.com/storage/v1/b/{bucket}/o/{encoded_key}?alt=media",
        headers={"Authorization": f"Bearer {token}"},
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return response.read()
    except urllib.error.HTTPError as error:
        if error.code == 404:
            raise FileNotFoundError(key) from error
        raise


def multipart_upload(bucket: str, metadata: dict[str, str], content: bytes, token: str) -> None:
    boundary = f"gestalt-sdk-docs-{uuid.uuid4().hex}"
    body = b"".join(
        [
            f"--{boundary}\r\n".encode("ascii"),
            b"Content-Type: application/json; charset=UTF-8\r\n\r\n",
            json.dumps(metadata, sort_keys=True).encode("utf-8"),
            b"\r\n",
            f"--{boundary}\r\n".encode("ascii"),
            f"Content-Type: {metadata['contentType']}\r\n\r\n".encode("ascii"),
            content,
            b"\r\n",
            f"--{boundary}--\r\n".encode("ascii"),
        ]
    )
    request = urllib.request.Request(
        f"https://storage.googleapis.com/upload/storage/v1/b/{bucket}/o?uploadType=multipart",
        data=body,
        method="POST",
        headers={
            "Authorization": f"Bearer {token}",
            "Content-Type": f"multipart/related; boundary={boundary}",
            "Content-Length": str(len(body)),
        },
    )
    with urllib.request.urlopen(request, timeout=60):
        pass


def cleanup_latest_prefix(
    *,
    bucket: str,
    language: str,
    keep_keys: set[str],
    token: str,
) -> None:
    prefix = f"api/{language}/latest/"
    for key in list_object_keys(bucket, prefix, token):
        if key not in keep_keys:
            delete_object(bucket, key, token)
            print(f"deleted stale latest object gs://{bucket}/{key}")


def list_object_keys(bucket: str, prefix: str, token: str) -> list[str]:
    keys: list[str] = []
    page_token: str | None = None
    while True:
        query = {
            "prefix": prefix,
            "fields": "items/name,nextPageToken",
        }
        if page_token:
            query["pageToken"] = page_token
        url = (
            f"https://storage.googleapis.com/storage/v1/b/{bucket}/o?"
            + urllib.parse.urlencode(query)
        )
        request = urllib.request.Request(url, headers={"Authorization": f"Bearer {token}"})
        with urllib.request.urlopen(request, timeout=30) as response:
            payload = json.load(response)
        keys.extend(item["name"] for item in payload.get("items", []))
        page_token = payload.get("nextPageToken")
        if not page_token:
            return keys


def delete_object(bucket: str, key: str, token: str) -> None:
    encoded_key = urllib.parse.quote(key, safe="")
    request = urllib.request.Request(
        f"https://storage.googleapis.com/storage/v1/b/{bucket}/o/{encoded_key}",
        method="DELETE",
        headers={"Authorization": f"Bearer {token}"},
    )
    try:
        with urllib.request.urlopen(request, timeout=30):
            pass
    except urllib.error.HTTPError as error:
        if error.code != 404:
            raise


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        sys.exit(130)
