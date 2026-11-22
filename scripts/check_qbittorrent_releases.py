#!/usr/bin/env python3
"""Find a newer official qBittorrent image for the compatibility matrix."""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path
from urllib import parse, request


TAG_RE = re.compile(r"^(\d+)\.(\d+)\.(\d+)(?:-(\d+))?$")


def version_key(tag: str) -> tuple[int, int, int, int]:
    match = TAG_RE.fullmatch(tag)
    if not match:
        raise ValueError(f"unsupported image tag {tag!r}")
    return tuple(int(value or 0) for value in match.groups())


def image_tags(repository: str) -> list[dict[str, object]]:
    url = "https://hub.docker.com/v2/repositories/" + parse.quote(repository, safe="/") + "/tags"
    url += "?page_size=100&ordering=last_updated"
    with request.urlopen(url, timeout=30) as response:
        payload = json.load(response)
    results = payload.get("results")
    if not isinstance(results, list):
        raise RuntimeError("Docker Hub returned an invalid tag response")
    return [row for row in results if isinstance(row, dict)]


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", default="qbittorrentofficial/qbittorrent-nox")
    parser.add_argument("--current", action="append", help="matrix tag, without image digest")
    parser.add_argument("--workflow", type=Path, help="read image tags from a CI workflow")
    parser.add_argument("--github-output", action="store_true")
    args = parser.parse_args()
    try:
        current = list(args.current or [])
        if args.workflow:
            workflow_text = args.workflow.read_text(encoding="utf-8")
            current.extend(
                re.findall(
                    r"image:\s+[^\s:]+:([0-9]+\.[0-9]+\.[0-9]+(?:-[0-9]+)?)(?:@sha256:[0-9a-f]+)?",
                    workflow_text,
                )
            )
        if not current:
            raise RuntimeError("provide --current or --workflow with at least one matrix image")
        current_keys = [version_key(tag) for tag in current]
        current_max = max(current_keys)
        candidates = []
        for row in image_tags(args.repository):
            tag = row.get("name")
            if not isinstance(tag, str) or not TAG_RE.fullmatch(tag):
                continue
            key = version_key(tag)
            if key > current_max:
                candidates.append((key, tag, row.get("digest", "")))
        if not candidates:
            print(f"No qBittorrent release newer than {max(current, key=version_key)} was found.")
            if args.github_output:
                print("newer=false")
            return 0
        key, tag, digest = max(candidates)
        if not isinstance(digest, str) or not digest.startswith("sha256:"):
            raise RuntimeError(f"Docker Hub did not return a digest for {tag}")
        image = f"{args.repository}:{tag}@{digest}"
        print(f"New qBittorrent release: {tag}")
        print(f"Immutable image: {image}")
        if args.github_output:
            print("newer=true")
            print(f"tag={tag}")
            print(f"digest={digest}")
            print(f"image={image}")
    except Exception as error:
        print(f"qBittorrent release check failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
