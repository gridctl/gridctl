"""Fixtures for per-package coverage checks against synthetic profiles."""

from pathlib import Path
import os
import stat
import subprocess
import tempfile
import unittest


MODULE = "github.com/gridctl/gridctl"
SCRIPT = "scripts/check-coverage.sh"
DASH = "\u2014"
THRESHOLDS = (
    ("pkg/controller", 50),
    ("pkg/runtime", 60),
    ("pkg/mcp", 75),
    ("pkg/reload", 60),
    ("pkg/config", 70),
)


def block(group, name, stmts, count, line=1):
    return f"{MODULE}/{group}/{name}.go:{line}.1,{line + 1}.1 {stmts} {count}"


def at_threshold():
    """Statement counts that land each group on its threshold exactly."""
    return [
        block("pkg/controller", "a", 1, 1, 1),
        block("pkg/controller", "b", 1, 0, 3),
        block("pkg/runtime/docker", "a", 3, 1, 1),
        block("pkg/runtime", "b", 2, 0, 3),
        block("pkg/mcp", "a", 3, 1, 1),
        block("pkg/mcp", "b", 1, 0, 3),
        block("pkg/reload", "a", 3, 1, 1),
        block("pkg/reload", "b", 2, 0, 3),
        block("pkg/config", "a", 7, 1, 1),
        block("pkg/config", "b", 3, 0, 3),
    ]


def ok_line(package, coverage, threshold):
    return f"OK    {package} {DASH} {coverage}% (threshold: {threshold}%)"


def fail_line(package, coverage, threshold):
    return f"FAIL  {package} {DASH} {coverage}% < {threshold}% threshold"


class CheckCoverageTests(unittest.TestCase):
    def run_script(self, *args, profile=None, env=None):
        with tempfile.TemporaryDirectory(dir=".", prefix=".coverage-test-") as directory:
            command = ["bash", SCRIPT, *args]
            if profile is not None:
                path = Path(directory) / "coverage.out"
                path.write_text(profile)
                command.append(str(path))
            result = subprocess.run(
                command, capture_output=True, text=True, timeout=10, env=env)
            return result, (str(path) if profile is not None else "")

    def assert_ok(self, result, expected):
        self.assertEqual(0, result.returncode, result.stdout + result.stderr)
        self.assertEqual("", result.stderr)
        lines = result.stdout.splitlines()
        self.assertEqual(expected, [line for line in lines if line.startswith("OK")])
        self.assertNotIn("FAIL", result.stdout)
        self.assertIn("All packages meet coverage thresholds.", result.stdout)

    def test_script_does_not_invoke_go(self):
        text = Path(SCRIPT).read_text()
        self.assertIn("set -euo pipefail", text)
        self.assertNotIn("go test", text)
        self.assertNotIn("go tool cover", text)

    def test_exact_thresholds_pass(self):
        expected = [ok_line(package, f"{threshold}.0", threshold)
                    for package, threshold in THRESHOLDS]
        body = "\n".join(at_threshold()) + "\n"
        for mode in ("set", "count", "atomic"):
            with self.subTest(mode=mode):
                result, _ = self.run_script(profile=f"mode: {mode}\n{body}")
                self.assert_ok(result, expected)

    def test_one_statement_below_fails(self):
        lines = [
            block("pkg/controller", "a", 1, 1, 1),
            block("pkg/controller", "b", 1, 0, 3),
            block("pkg/runtime/docker", "a", 3, 1, 1),
            block("pkg/runtime", "b", 2, 0, 3),
            # 74 of 100 statements is one statement under the 75% minimum.
            block("pkg/mcp", "covered", 74, 1, 1),
            block("pkg/mcp", "uncovered", 26, 0, 3),
            block("pkg/reload", "a", 3, 1, 1),
            block("pkg/reload", "b", 2, 0, 3),
            block("pkg/config", "a", 7, 1, 1),
            block("pkg/config", "b", 3, 0, 3),
        ]
        result, _ = self.run_script(profile="mode: atomic\n" + "\n".join(lines) + "\n")
        self.assertEqual(1, result.returncode, result.stdout + result.stderr)
        self.assertEqual("", result.stderr)
        self.assertIn(fail_line("pkg/mcp", "74.0", 75), result.stdout)
        self.assertIn(ok_line("pkg/controller", "50.0", 50), result.stdout)
        self.assertIn("Per-package coverage check failed.", result.stdout)
        self.assertNotIn("All packages meet coverage thresholds.", result.stdout)

    def test_mcpauth_zero_coverage_does_not_affect_mcp(self):
        lines = at_threshold() + [block("pkg/mcpauth", "a", 100, 0, 1)]
        result, _ = self.run_script(profile="mode: set\n" + "\n".join(lines) + "\n")
        self.assert_ok(result, [ok_line(package, f"{threshold}.0", threshold)
                                for package, threshold in THRESHOLDS])
        self.assertIn(ok_line("pkg/mcp", "75.0", 75), result.stdout)

    def test_runtime_docker_blocks_count_toward_runtime(self):
        lines = [line for line in at_threshold() if "/pkg/runtime/" not in line]
        lines.extend([
            block("pkg/runtime/docker", "a", 3, 1, 1),
            block("pkg/runtime/docker", "b", 2, 0, 3),
        ])
        result, _ = self.run_script(profile="mode: atomic\n" + "\n".join(lines) + "\n")
        self.assertEqual(0, result.returncode, result.stdout + result.stderr)
        self.assertIn(ok_line("pkg/runtime", "60.0", 60), result.stdout)
        self.assertNotIn("no coverage blocks", result.stdout)

    def test_duplicate_blocks_merge_per_mode(self):
        others = [line for line in at_threshold() if "/pkg/controller/" not in line]
        cases = {
            # Last hit is zero. set must keep the earlier hit (maximum), not
            # the last line, and must not sum the statement counts.
            "set": [
                block("pkg/controller", "a", 1, 1, 1),
                block("pkg/controller", "a", 1, 0, 1),
                block("pkg/controller", "b", 1, 0, 3),
            ],
            # First hit is zero. count and atomic sum, so the block is covered.
            # Summing statement counts would print 66.7 instead of 50.0.
            "count": [
                block("pkg/controller", "a", 1, 0, 1),
                block("pkg/controller", "a", 1, 1, 1),
                block("pkg/controller", "b", 1, 0, 3),
            ],
            "atomic": [
                block("pkg/controller", "a", 1, 0, 1),
                block("pkg/controller", "a", 1, 1, 1),
                block("pkg/controller", "b", 1, 0, 3),
            ],
        }
        for mode, controller in cases.items():
            with self.subTest(mode=mode):
                result, _ = self.run_script(
                    profile="mode: " + mode + "\n" + "\n".join(controller + others) + "\n")
                self.assertEqual(0, result.returncode, result.stdout + result.stderr)
                self.assertIn(ok_line("pkg/controller", "50.0", 50), result.stdout)
                self.assertNotIn("66.7", result.stdout)
                self.assertNotIn(f"{DASH} 0.0%", result.stdout)

    def test_missing_group_fails(self):
        lines = [line for line in at_threshold() if "/pkg/config/" not in line]
        result, path = self.run_script(profile="mode: set\n" + "\n".join(lines) + "\n")
        self.assertEqual(1, result.returncode, result.stdout + result.stderr)
        self.assertEqual("", result.stderr)
        self.assertIn(
            f"FAIL  pkg/config: no coverage blocks under {MODULE}/pkg/config/ in {path}",
            result.stdout)
        self.assertIn(ok_line("pkg/mcp", "75.0", 75), result.stdout)
        self.assertIn("Per-package coverage check failed.", result.stdout)

    def test_missing_argument_exits_2(self):
        cases = (
            ("no-args", []),
            ("empty", [""]),
            ("missing-file", ["scripts/.coverage-test-missing.out"]),
        )
        for name, args in cases:
            with self.subTest(name=name):
                result, _ = self.run_script(*args)
                self.assertEqual(2, result.returncode, result.stdout + result.stderr)
                self.assertEqual("", result.stdout)
                self.assertEqual(
                    "usage: check-coverage.sh <coverage profile>\n", result.stderr)

    def test_unreadable_profile_exits_2(self):
        with tempfile.TemporaryDirectory(dir=".", prefix=".coverage-test-") as directory:
            path = Path(directory) / "coverage.out"
            path.write_text("mode: set\n")
            path.chmod(0)
            try:
                if os.access(path, os.R_OK):
                    self.skipTest("running as a user that can read mode 000")
                result = subprocess.run(
                    ["bash", SCRIPT, str(path)],
                    capture_output=True, text=True, timeout=10)
            finally:
                path.chmod(stat.S_IRUSR | stat.S_IWUSR)
            self.assertEqual(2, result.returncode, result.stdout + result.stderr)
            self.assertEqual("", result.stdout)
            self.assertIn("usage: check-coverage.sh <coverage profile>", result.stderr)

    def test_missing_mode_header_exits_1(self):
        cases = (
            ("no-header", "\n".join(at_threshold()) + "\n"),
            ("unknown-mode", "mode: percent\n" + "\n".join(at_threshold()) + "\n"),
            ("empty", ""),
        )
        for name, profile in cases:
            with self.subTest(name=name):
                result, path = self.run_script(profile=profile)
                self.assertEqual(1, result.returncode, result.stdout + result.stderr)
                self.assertEqual("", result.stdout)
                self.assertEqual(
                    f"FAIL  {path} has no recognized mode header\n", result.stderr)

    def test_order_and_thresholds_name_the_same_packages(self):
        text = Path(SCRIPT).read_text()
        order = []
        thresholds = []
        for line in text.splitlines():
            stripped = line.strip()
            if stripped.startswith("ORDER=(") and stripped.endswith(")"):
                order = stripped[len("ORDER=("):-1].split()
            if stripped.startswith('["pkg/') and "]=" in stripped:
                thresholds.append(stripped.split('"', 2)[1])
        expected = [package for package, _ in THRESHOLDS]
        self.assertEqual(expected, order)
        self.assertEqual(expected, thresholds)
        self.assertEqual(order, thresholds)
        self.assertIn("check-coverage.sh: ORDER and THRESHOLDS differ", text)

    def test_does_not_call_go_when_go_is_hostile(self):
        with tempfile.TemporaryDirectory(dir=".", prefix=".coverage-test-") as directory:
            root = Path(directory)
            fake = root / "go"
            fake.write_text("#!/bin/sh\necho go-invoked >&2\nexit 99\n")
            fake.chmod(0o755)
            env = dict(os.environ, PATH=f"{root}:{os.environ.get('PATH', '')}")
            result, _ = self.run_script(
                profile="mode: set\n" + "\n".join(at_threshold()) + "\n", env=env)
            self.assertEqual(0, result.returncode, result.stdout + result.stderr)
            self.assertNotIn("go-invoked", result.stderr)


if __name__ == "__main__":
    unittest.main()
