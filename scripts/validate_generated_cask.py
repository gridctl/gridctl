#!/usr/bin/env python3
"""Validate the generated Gridctl cask's postflight steps contract."""

import argparse
from pathlib import Path
import re
import sys


LEGACY_POSTFLIGHT = re.compile(r"^\s*postflight\s+do\s*$", re.MULTILINE)
POSTFLIGHT_STEPS = re.compile(r"^\s*postflight_steps\s+do\s*$", re.MULTILINE)
EXPECTED_STEP = re.compile(
    r'^\s*postflight_steps do\s*\n'
    r'\s+on_macos do\s*\n'
    r'\s+run "/usr/bin/xattr",\s*\n'
    r'\s+args: \["-dr", "com\.apple\.quarantine", "\{\{staged_path\}\}/gridctl"\]\s*\n'
    r'\s+end\s*\n'
    r'\s*end\s*$',
    re.MULTILINE,
)
XATTR_COMMAND = 'run "/usr/bin/xattr"'
STAGED_PATH = "{{staged_path}}/gridctl"
ARCHIVE_SUFFIXES = (
    "_darwin_amd64.tar.gz",
    "_darwin_arm64.tar.gz",
    "_linux_amd64.tar.gz",
    "_linux_arm64.tar.gz",
)
LEGACY_BASELINE_ERRORS = [
    "legacy postflight declaration is forbidden",
    "expected exactly one postflight_steps declaration, found 0",
    "expected exactly one xattr run step, found 0",
    "system_command is forbidden in postflight steps",
    "expected exactly one literal {{staged_path}}/gridctl token",
    "expected macOS-gated xattr postflight step is missing",
]


def validation_errors(cask: str) -> list[str]:
    """Return contract violations found in a complete generated Gridctl cask."""
    errors = []
    if not re.search(r'^cask "gridctl" do\s*$', cask, re.MULTILINE):
        errors.append('missing cask "gridctl" declaration')
    for suffix in ARCHIVE_SUFFIXES:
        url = re.compile(
            r'^\s*url "https://github\.com/gridctl/gridctl/releases/download/'
            r'[^"/]+/gridctl_#\{version\}' + re.escape(suffix) + r'"\s*$',
            re.MULTILINE,
        )
        count = len(url.findall(cask))
        if count != 1:
            errors.append(f"expected exactly one production {suffix} asset URL, found {count}")
    checksums = len(re.findall(r'^\s*sha256 "[0-9a-f]{64}"\s*$', cask, re.MULTILINE))
    if checksums != 4:
        errors.append(f"expected exactly four archive checksums, found {checksums}")
    if LEGACY_POSTFLIGHT.search(cask):
        errors.append("legacy postflight declaration is forbidden")

    steps = len(POSTFLIGHT_STEPS.findall(cask))
    if steps != 1:
        errors.append(f"expected exactly one postflight_steps declaration, found {steps}")

    commands = cask.count(XATTR_COMMAND)
    if commands != 1:
        errors.append(f"expected exactly one xattr run step, found {commands}")
    if "system_command" in cask:
        errors.append("system_command is forbidden in postflight steps")
    if cask.count(STAGED_PATH) != 1:
        errors.append("expected exactly one literal {{staged_path}}/gridctl token")
    if EXPECTED_STEP.search(cask) is None:
        errors.append("expected macOS-gated xattr postflight step is missing")

    without_staged_path = cask.replace(STAGED_PATH, "")
    if "{{" in without_staged_path or "}}" in without_staged_path:
        errors.append("unresolved GoReleaser template expression remains")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Validate a complete GoReleaser-generated Gridctl cask."
    )
    parser.add_argument("cask", type=Path)
    args = parser.parse_args()

    try:
        cask = args.cask.read_text()
    except OSError as error:
        print(f"generated cask validation failed: {error}", file=sys.stderr)
        return 1

    errors = validation_errors(cask)
    if errors:
        for error in errors:
            print(f"generated cask validation failed: {error}", file=sys.stderr)
        return 1
    print(f"generated cask validation passed: {args.cask}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
