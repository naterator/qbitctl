#!/usr/bin/env python3
"""Turn govulncheck's streaming JSON output into an issue-ready report."""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import re
import sys
from typing import Any


_OUTPUT_VALUE = re.compile(r"^[A-Za-z0-9_.:,/-]*$")


def _messages(path: Path) -> list[dict[str, Any]]:
    text = path.read_text(encoding="utf-8")
    decoder = json.JSONDecoder()
    messages: list[dict[str, Any]] = []
    offset = 0
    while True:
        while offset < len(text) and text[offset].isspace():
            offset += 1
        if offset == len(text):
            return messages
        try:
            value, offset = decoder.raw_decode(text, offset)
        except json.JSONDecodeError as error:
            raise ValueError(f"invalid govulncheck JSON at byte {error.pos}") from error
        if not isinstance(value, dict):
            raise ValueError("govulncheck JSON message is not an object")
        messages.append(value)


def _text(value: Any) -> str:
    return value.strip() if isinstance(value, str) else ""


def _finding_rows(messages: list[dict[str, Any]]) -> list[dict[str, str]]:
    if not messages:
        raise ValueError("govulncheck emitted an empty JSON stream")
    config = messages[0].get("config")
    if not isinstance(config, dict) or not _text(config.get("protocol_version")):
        raise ValueError("govulncheck JSON stream has no configuration message")

    rows: set[tuple[str, str, str, str, str, str]] = set()
    for message in messages:
        if "finding" not in message:
            continue
        finding = message["finding"]
        if not isinstance(finding, dict):
            raise ValueError("govulncheck finding is not an object")
        osv = _text(finding.get("osv"))
        if not osv:
            raise ValueError("govulncheck finding has no OSV identifier")
        fixed = _text(finding.get("fixed_version"))
        trace = finding.get("trace") or []
        if not isinstance(trace, list):
            raise ValueError("govulncheck finding trace is not an array")
        frame = trace[0] if trace and isinstance(trace[0], dict) else {}
        # At symbol scan level govulncheck can emit module/package hints before
        # it has established a reachable vulnerable function. Do not turn
        # those hints into actionable findings.
        symbol = _text(frame.get("function")) or _text(frame.get("symbol"))
        if not symbol:
            continue
        module = frame.get("module")
        if isinstance(module, dict):
            module_path = _text(module.get("path"))
            module_version = _text(module.get("version"))
        else:
            module_path = _text(module)
            module_version = _text(frame.get("version"))
        package = _text(frame.get("package"))
        rows.add((osv, fixed, module_path, module_version, package, symbol))

    return [
        {
            "osv": row[0],
            "fixed": row[1],
            "module": row[2],
            "version": row[3],
            "package": row[4],
            "symbol": row[5],
        }
        for row in sorted(rows)
    ]


def _markdown_value(value: str) -> str:
    return value.replace("`", "\\`").replace("\n", " ")


def _write_outputs(path: Path, rows: list[dict[str, str]]) -> None:
    identifiers = sorted({row["osv"] for row in rows})
    joined = "\n".join(identifiers).encode("utf-8")
    fingerprint = hashlib.sha256(joined).hexdigest()[:16] if identifiers else "none"
    values = {
        "has_findings": "true" if identifiers else "false",
        "finding_count": str(len(identifiers)),
        "finding_fingerprint": fingerprint,
        "finding_ids": ",".join(identifiers),
    }
    for key, value in values.items():
        if not _OUTPUT_VALUE.fullmatch(value):
            raise ValueError(f"unsafe output value for {key}")
    path.write_text(
        "".join(f"{key}={value}\n" for key, value in values.items()),
        encoding="utf-8",
    )


def _write_report(path: Path, rows: list[dict[str, str]]) -> None:
    identifiers = sorted({row["osv"] for row in rows})
    lines = [
        "# Go vulnerability findings",
        "",
        f"govulncheck reported {len(identifiers)} distinct vulnerability identifier(s).",
        "",
    ]
    if not rows:
        lines.append("No reachable vulnerabilities were reported.")
    else:
        for row in rows:
            fixed = f" (fixed in `{_markdown_value(row['fixed'])}`)" if row["fixed"] else ""
            lines.append(f"- `{_markdown_value(row['osv'])}`{fixed}")
            location = []
            if row["module"]:
                module = f"`{_markdown_value(row['module'])}`"
                if row["version"]:
                    module += f" `{_markdown_value(row['version'])}`"
                location.append(f"module {module}")
            if row["package"]:
                location.append(f"package `{_markdown_value(row['package'])}`")
            if row["symbol"]:
                location.append(f"symbol `{_markdown_value(row['symbol'])}`")
            if location:
                lines.append(f"  - {', '.join(location)}")
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", required=True, type=Path)
    parser.add_argument("--report", required=True, type=Path)
    parser.add_argument("--outputs", required=True, type=Path)
    args = parser.parse_args()
    try:
        rows = _finding_rows(_messages(args.input))
        _write_report(args.report, rows)
        _write_outputs(args.outputs, rows)
    except (OSError, ValueError, UnicodeError) as error:
        print(f"govulncheck report failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
