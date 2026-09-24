#!/usr/bin/env python3
"""Generate a cask with pinned GoReleaser and apply the postflight contract."""

import argparse
import hashlib
import importlib.util
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys
import tempfile


VALIDATOR_SPEC = importlib.util.spec_from_file_location(
    "validate_generated_cask", Path(__file__).with_name("validate_generated_cask.py")
)
validator = importlib.util.module_from_spec(VALIDATOR_SPEC)
VALIDATOR_SPEC.loader.exec_module(validator)
validation_errors = validator.validation_errors


GORELEASER_VERSION = "2.14.3"


def production_cask_config(source: str) -> str:
    """Extract the complete homebrew_casks section from production config."""
    match = re.search(r"^homebrew_casks:\s*$", source, re.MULTILINE)
    if match is None:
        raise ValueError("production config has no homebrew_casks section")
    suffix = source[match.start():]
    if not suffix.endswith("\n"):
        suffix += "\n"
    return suffix


def fixture_config(cask_config: str, output: Path) -> str:
    """Build a minimal release config around the unmodified production cask section."""
    return f'''version: 2
dist: "{output}"
project_name: gridctl
builds:
  - id: gridctl
    main: ./cmd/scenarioverify
    binary: gridctl
    env:
      - CGO_ENABLED=0
    goos: [linux, darwin]
    goarch: [amd64, arm64]
archives:
  - id: default
    formats: [tar.gz]
    name_template: "{{{{ .ProjectName }}}}_{{{{ .Version }}}}_{{{{ .Os }}}}_{{{{ .Arch }}}}"
checksum:
  name_template: checksums.txt
changelog:
  disable: true
release:
  github:
    owner: gridctl
    name: gridctl
  skip_upload: true
''' + cask_config


def require_linux_amd64() -> None:
    system = platform.system()
    machine = platform.machine()
    if system != "Linux" or machine != "x86_64":
        raise ValueError(f"generator regression requires Linux-x86_64, found {system}-{machine}")


def goreleaser_version(executable: Path) -> str:
    completed = subprocess.run(
        [str(executable), "--version"], check=True, text=True, capture_output=True
    )
    output = completed.stdout + completed.stderr
    match = re.search(r"(?:GitVersion|version):?\s*v?(\d+\.\d+\.\d+)", output, re.IGNORECASE)
    if match is None:
        raise ValueError(f"cannot determine GoReleaser version from: {output.strip()}")
    if match.group(1) != GORELEASER_VERSION:
        raise ValueError(
            f"GoReleaser {GORELEASER_VERSION} is required, found {match.group(1)}"
        )
    return output.strip()


def archive_checksum_errors(cask: str, output: Path) -> list[str]:
    """Compare generated cask checksums with all four generated archives."""
    version_match = re.search(r'^\s*version "([^"]+)"\s*$', cask, re.MULTILINE)
    if version_match is None:
        return ["generated cask has no version declaration"]
    version = version_match.group(1)
    errors = []
    for suffix in validator.ARCHIVE_SUFFIXES:
        checksum = re.search(
            r'^\s*url "[^"]+/gridctl_#\{version\}' + re.escape(suffix) + r'"\s*\n'
            r'^\s*sha256 "([0-9a-f]{64})"\s*$',
            cask,
            re.MULTILINE,
        )
        archive = output / f"gridctl_{version}{suffix}"
        if checksum is None:
            errors.append(f"generated cask has no checksum for {suffix}")
        elif not archive.is_file():
            errors.append(f"generated archive is missing: {archive.name}")
        else:
            actual = hashlib.sha256(archive.read_bytes()).hexdigest()
            if checksum.group(1) != actual:
                errors.append(f"generated cask checksum does not match {archive.name}")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Exercise the production-derived cask config with pinned GoReleaser."
    )
    parser.add_argument("--goreleaser", type=Path, default=Path("goreleaser"))
    parser.add_argument(
        "--expect-invalid",
        action="store_true",
        help="pass only when the generated cask is rejected (baseline migration check)",
    )
    parser.add_argument("--output-cask", type=Path)
    args = parser.parse_args()

    root = Path(__file__).resolve().parent.parent
    try:
        require_linux_amd64()
        version = goreleaser_version(args.goreleaser.resolve())
        source = (root / ".goreleaser.yaml").read_text()
        cask_config = production_cask_config(source)
        with tempfile.TemporaryDirectory(prefix="gridctl-cask-") as temporary:
            directory = Path(temporary)
            config = directory / "goreleaser.yaml"
            output = directory / "dist"
            config.write_text(fixture_config(cask_config, output))
            environment = {
                **os.environ,
                "GH_TOKEN": "",
                "GITHUB_TOKEN": "",
                "GORELEASER_TOKEN": "",
            }
            command = [
                str(args.goreleaser.resolve()),
                "release",
                "--snapshot",
                "--clean",
                "--skip=before,sbom",
                "--config",
                str(config),
            ]
            subprocess.run(command, cwd=root, env=environment, check=True)
            cask_path = output / "homebrew" / "Casks" / "gridctl.rb"
            cask = cask_path.read_text()
            errors = validation_errors(cask) + archive_checksum_errors(cask, output)
            if args.output_cask:
                args.output_cask.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(cask_path, args.output_cask)

        digest = hashlib.sha256(cask.encode()).hexdigest()
        print(f"environment: {platform.platform()} ({platform.machine()})")
        print(f"generator: {version}")
        print(f"generated cask sha256: {digest}")
        if errors:
            for error in errors:
                print(f"generated cask validation failed: {error}")
        if args.expect_invalid:
            if errors != validator.LEGACY_BASELINE_ERRORS:
                print("baseline did not produce the exact legacy-hook rejection", file=sys.stderr)
                return 1
            print("baseline rejection observed")
            return 0
        if errors:
            return 1
        print("generated cask validation passed")
        return 0
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f"generator regression failed: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
