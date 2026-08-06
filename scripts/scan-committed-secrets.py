#!/usr/bin/env python3
"""High-confidence committed-secret scan without printing secret values."""
from __future__ import annotations

import math
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
MAX_BYTES = 2 << 20
SKIP_PREFIXES = (
    "third_party/", "docs/reference/", ".git/", "artifacts/",
)
SKIP_SUFFIXES = (".zip", ".bundle", ".png", ".jpg", ".jpeg", ".gif", ".pdf", ".woff", ".woff2")
PATTERNS = [
    ("private key", re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH |PGP )?PRIVATE KEY-----")),
    ("AWS access key", re.compile(r"\b(?:AKIA|ASIA)[A-Z0-9]{16}\b")),
    ("GitHub token", re.compile(r"\bgh(?:p|o|u|s|r)_[A-Za-z0-9]{30,}\b")),
    ("Slack token", re.compile(r"\bxox[baprs]-[A-Za-z0-9-]{20,}\b")),
    ("Google API key", re.compile(r"\bAIza[0-9A-Za-z_-]{35}\b")),
]
ASSIGNMENT = re.compile(
    r"(?i)\b(password|passwd|secret|token|api[_-]?key|private[_-]?key)\b\s*[:=]\s*[\"']([^\"'\r\n]{20,})[\"']"
)
ALLOW_FRAGMENTS = (
    "development", "example", "placeholder", "change-me", "replace-me", "test-only",
    "${", "/run/secrets/", "sha256:", "00000000-", "0123456789",
)


def entropy(value: str) -> float:
    if not value:
        return 0.0
    counts = {character: value.count(character) for character in set(value)}
    length = len(value)
    return -sum((count / length) * math.log2(count / length) for count in counts.values())


def tracked_files() -> list[str]:
    # Include pending untracked source files so the release gate protects the
    # exact candidate worktree before commit, not only the previous commit.
    result = subprocess.run(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        cwd=ROOT,
        check=True,
        stdout=subprocess.PIPE,
    )
    return [entry.decode("utf-8") for entry in result.stdout.split(b"\0") if entry]


def main() -> int:
    findings: list[tuple[str, int, str]] = []
    for relative in tracked_files():
        if relative.startswith(SKIP_PREFIXES) or relative.lower().endswith(SKIP_SUFFIXES):
            continue
        path = ROOT / relative
        try:
            if not path.is_file() or path.stat().st_size > MAX_BYTES:
                continue
            text = path.read_text(encoding="utf-8")
        except (OSError, UnicodeDecodeError):
            continue
        for line_number, line in enumerate(text.splitlines(), 1):
            for label, pattern in PATTERNS:
                if pattern.search(line):
                    findings.append((relative, line_number, label))
            for match in ASSIGNMENT.finditer(line):
                value = match.group(2).strip()
                lowered = value.lower()
                if any(fragment in lowered for fragment in ALLOW_FRAGMENTS):
                    continue
                compact = re.sub(r"[^A-Za-z0-9+/=_-]", "", value)
                if len(compact) >= 24 and entropy(compact) >= 3.5:
                    findings.append((relative, line_number, "high-entropy credential assignment"))
    if findings:
        for path, line, label in sorted(set(findings)):
            print(f"ERROR: possible {label} in {path}:{line}", file=sys.stderr)
        return 1
    print("Committed-secret scan passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
