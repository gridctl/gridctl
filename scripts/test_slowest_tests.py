"""Behavior tests for the slowest-tests summary.

The workflow step runs under bash -e without pipefail. A jq | sort | head
pipeline would report head's status, so these tests require the script to
notice jq and sort failures before any pipeline and to exit 0 either way.
"""

import json
import os
from pathlib import Path
import stat
import subprocess
import tempfile
import unittest


SCRIPT = "scripts/slowest-tests.sh"
NOTICE = "slowest-tests summary failed; see the capture on the runner"
HEADER = "Slowest top-level tests (elapsed seconds, result, name):"


def event(name, elapsed, action="pass", output=None, package=None):
    body = {"Action": action, "Test": name, "Elapsed": elapsed}
    if output is not None:
        body["Output"] = output
    if package is not None:
        body["Package"] = package
    return body


class SlowestTests(unittest.TestCase):
    def run_script(self, *args, env=None):
        return subprocess.run(
            ["bash", SCRIPT, *args],
            capture_output=True, text=True, timeout=10, env=env)

    def write_capture(self, directory, events, raw=None):
        path = Path(directory) / "capture.json"
        if raw is not None:
            path.write_text(raw)
        else:
            path.write_text("".join(json.dumps(item) + "\n" for item in events))
        return path

    def test_script_does_not_pipeline_jq_or_sort(self):
        text = Path(SCRIPT).read_text()
        self.assertIn("set -euo pipefail", text)
        self.assertIn("exit 0", text)
        self.assertNotIn("| head", text)
        self.assertNotIn("| sort", text)
        self.assertNotIn(".Output", text)

    def test_missing_capture_notice(self):
        missing = "scripts/.slowest-test-missing.json"
        cases = {
            "no-args": [],
            "empty-arg": [""],
            "missing-file": [missing],
        }
        with tempfile.TemporaryDirectory(dir=".", prefix=".slowest-test-") as directory:
            empty = Path(directory) / "empty.json"
            empty.write_text("")
            cases["empty-file"] = [str(empty)]
            for name, args in cases.items():
                with self.subTest(name=name):
                    result = self.run_script(*args)
                    self.assertEqual(0, result.returncode, result.stdout + result.stderr)
                    self.assertIn("no capture at ", result.stdout)
                    self.assertNotIn(NOTICE, result.stdout)
                    self.assertNotIn(HEADER, result.stdout)
                    self.assertEqual("", result.stderr)

    def test_corrupt_capture_notice_hides_partial_rows(self):
        raw = "\n".join([
            json.dumps(event("TestA", 1.5)),
            json.dumps(event("TestA/sub", 9)),
            "not json",
            json.dumps(event("TestB", 3.2, action="fail")),
            "",
        ])
        with tempfile.TemporaryDirectory(dir=".", prefix=".slowest-test-") as directory:
            path = self.write_capture(directory, None, raw=raw)
            before = path.read_bytes()
            result = self.run_script(str(path))
            self.assertEqual(0, result.returncode, result.stdout + result.stderr)
            self.assertEqual(NOTICE + "\n", result.stdout)
            self.assertNotIn("TestA", result.stdout)
            self.assertNotIn("TestB", result.stdout)
            self.assertNotIn(HEADER, result.stdout)
            self.assertIn("parse error", result.stderr)
            self.assertEqual(before, path.read_bytes())

    def test_descending_order_and_limit(self):
        events = [event(f"Test{index:02d}", index) for index in range(30)]
        events.append(event("Test29/sub", 1000))
        with tempfile.TemporaryDirectory(dir=".", prefix=".slowest-test-") as directory:
            path = self.write_capture(directory, events)
            result = self.run_script(str(path))
            self.assertEqual(0, result.returncode, result.stdout + result.stderr)
            self.assertEqual("", result.stderr)
            lines = result.stdout.splitlines()
            self.assertEqual(HEADER, lines[0])
            rows = lines[1:]
            self.assertEqual(25, len(rows))
            self.assertEqual("29\tpass\tTest29", rows[0])
            self.assertEqual("5\tpass\tTest05", rows[-1])
            self.assertNotIn("Test04", result.stdout)
            self.assertNotIn("Test29/sub", result.stdout)
            elapsed = [float(row.split("\t", 1)[0]) for row in rows]
            self.assertEqual(sorted(elapsed, reverse=True), elapsed)

    def test_decimal_order(self):
        events = [
            event("TestSlow", 1.0),
            event("TestFast", 30),
            event("TestMid", 10.5, action="fail"),
        ]
        with tempfile.TemporaryDirectory(dir=".", prefix=".slowest-test-") as directory:
            path = self.write_capture(directory, events)
            result = self.run_script(str(path))
            self.assertEqual(0, result.returncode, result.stdout + result.stderr)
            self.assertEqual([
                HEADER,
                "30\tpass\tTestFast",
                "10.5\tfail\tTestMid",
                "1.0\tpass\tTestSlow",
            ], result.stdout.splitlines())

    def test_subtests_excluded(self):
        events = [
            event("TestParent", 1.25),
            event("TestParent/child", 99),
        ]
        with tempfile.TemporaryDirectory(dir=".", prefix=".slowest-test-") as directory:
            path = self.write_capture(directory, events)
            result = self.run_script(str(path))
            self.assertEqual(0, result.returncode, result.stdout + result.stderr)
            self.assertIn("1.25\tpass\tTestParent", result.stdout)
            self.assertNotIn("TestParent/child", result.stdout)
            self.assertNotIn("99", result.stdout)

    def test_output_payloads_and_other_actions_excluded(self):
        events = [
            event("TestKept", 2, output="SECRET_ON_PASS", package="github.com/gridctl/gridctl/pkg/mcp"),
            {"Action": "output", "Test": "TestKept", "Output": "SECRET_OUTPUT_EVENT"},
            event("TestSkipped", 50, action="skip", output="SECRET_SKIP"),
            {"Action": "pass", "Elapsed": 40},
        ]
        with tempfile.TemporaryDirectory(dir=".", prefix=".slowest-test-") as directory:
            path = self.write_capture(directory, events)
            result = self.run_script(str(path))
            self.assertEqual(0, result.returncode, result.stdout + result.stderr)
            self.assertEqual([
                HEADER,
                "2\tpass\tTestKept",
            ], result.stdout.splitlines())
            for secret in ("SECRET_ON_PASS", "SECRET_OUTPUT_EVENT", "SECRET_SKIP", "pkg/mcp", "TestSkipped"):
                self.assertNotIn(secret, result.stdout)
                self.assertNotIn(secret, result.stderr)

    def test_hostile_jq_notice(self):
        with tempfile.TemporaryDirectory(dir=".", prefix=".slowest-test-") as directory:
            root = Path(directory)
            fake = root / "jq"
            fake.write_text("#!/bin/sh\necho '9.9\tpass\tShouldNotAppear'\necho jq-stub-failed >&2\nexit 1\n")
            fake.chmod(0o755)
            path = self.write_capture(root, [event("TestReal", 1)])
            env = dict(os.environ, PATH=f"{root}:{os.environ.get('PATH', '')}")
            result = self.run_script(str(path), env=env)
            self.assertEqual(0, result.returncode, result.stdout + result.stderr)
            self.assertEqual(NOTICE + "\n", result.stdout)
            self.assertNotIn("ShouldNotAppear", result.stdout)
            self.assertNotIn("TestReal", result.stdout)
            self.assertIn("jq-stub-failed", result.stderr)

    def test_hostile_sort_notice(self):
        with tempfile.TemporaryDirectory(dir=".", prefix=".slowest-test-") as directory:
            root = Path(directory)
            fake = root / "sort"
            fake.write_text("#!/bin/sh\necho '9.9\tpass\tSortedStub'\necho sort-stub-failed >&2\nexit 1\n")
            fake.chmod(0o755)
            path = self.write_capture(root, [event("TestReal", 4)])
            env = dict(os.environ, PATH=f"{root}:{os.environ.get('PATH', '')}")
            result = self.run_script(str(path), env=env)
            self.assertEqual(0, result.returncode, result.stdout + result.stderr)
            self.assertEqual(NOTICE + "\n", result.stdout)
            self.assertNotIn("TestReal", result.stdout)
            self.assertNotIn("SortedStub", result.stdout)
            self.assertIn("sort-stub-failed", result.stderr)

    def test_read_only_capture_is_unchanged(self):
        with tempfile.TemporaryDirectory(dir=".", prefix=".slowest-test-") as directory:
            path = self.write_capture(directory, [event("TestKept", 3)])
            path.chmod(stat.S_IRUSR | stat.S_IRGRP | stat.S_IROTH)
            before = path.read_bytes()
            try:
                result = self.run_script(str(path))
            finally:
                path.chmod(stat.S_IRUSR | stat.S_IWUSR)
            self.assertEqual(0, result.returncode, result.stdout + result.stderr)
            self.assertIn("3\tpass\tTestKept", result.stdout)
            self.assertEqual(before, path.read_bytes())

    def test_bash_e_step_still_exits_0(self):
        wrapper = (
            'bash scripts/slowest-tests.sh "$1" '
            '|| echo "slowest-tests summary failed; see the capture on the runner"\n'
            "exit 0\n"
        )
        with tempfile.TemporaryDirectory(dir=".", prefix=".slowest-test-") as directory:
            path = self.write_capture(directory, None, raw="not json\n")
            result = subprocess.run(
                ["bash", "-e", "-c", wrapper, "bash", str(path)],
                capture_output=True, text=True, timeout=10)
            self.assertEqual(0, result.returncode, result.stdout + result.stderr)
            self.assertEqual(1, result.stdout.count(NOTICE))
            self.assertNotIn(HEADER, result.stdout)


if __name__ == "__main__":
    unittest.main()
