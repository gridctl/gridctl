"""Exercise audit workflow shell steps with npm result fixtures."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[1]


class AuditWorkflowTests(unittest.TestCase):
    def run_step(self, name, report, status):
        workflow = yaml.safe_load(
            (ROOT / ".github/workflows/npm-audit-fix.yaml").read_text()
        )
        step = next(
            step for step in workflow["jobs"]["audit-fix"]["steps"]
            if step.get("name") == name
        )
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            npm = path / "npm"
            npm.write_text('#!/bin/bash\nprintf "%s\\n" "$REPORT"\nexit "$STATUS"\n')
            npm.chmod(0o755)
            env_file = path / "env"
            summary = path / "summary"
            env = {
                **os.environ,
                "PATH": f"{path}:{os.environ['PATH']}",
                "REPORT": json.dumps(report) if isinstance(report, dict) else report,
                "STATUS": str(status),
                "TOTAL": "6",
                "GITHUB_ENV": str(env_file),
                "GITHUB_STEP_SUMMARY": str(summary),
            }
            # Isolate the workflow's temporary outputs for parallel test runs.
            result = subprocess.run(
                ["bash", "-eo", "pipefail", "-c", step["run"].replace("/tmp/", f"{path}/")],
                env=env, capture_output=True, text=True, check=False,
            )
            return (
                result,
                env_file.read_text() if env_file.exists() else "",
                summary.read_text() if summary.exists() else "",
            )

    def test_partial_fixes_continue_and_report_remaining(self):
        result, env, summary = self.run_step(
            "Apply compatible fixes",
            {"audit": {"metadata": {"vulnerabilities": {"total": 5}}}}, 1,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("REMAINING=5", env)
        self.assertIn("remaining: 5", summary)

    def test_clean_fix_succeeds(self):
        result, env, _ = self.run_step(
            "Apply compatible fixes",
            {"audit": {"metadata": {"vulnerabilities": {"total": 0}}}}, 0,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("REMAINING=0", env)

    def test_initial_advisories_allow_exit_one(self):
        result, env, _ = self.run_step(
            "Capture current advisories",
            {"metadata": {"vulnerabilities": {"total": 6}}}, 1,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("TOTAL=6", env)

    def test_errors_and_invalid_reports_fail(self):
        for name in ("Capture current advisories", "Apply compatible fixes"):
            valid = {"metadata": {"vulnerabilities": {"total": 5}}}
            if name == "Apply compatible fixes":
                valid = {"audit": valid}
            for report, status in (
                ({"error": {"code": "ERESOLVE"}}, 1),
                ({**valid, "error": {"code": "EIO"}}, 1),
                ({"audit": {"error": {"code": "ENOTFOUND"}}}, 1),
                ("not json", 1),
                ({}, 0),
                (valid, 2),
            ):
                with self.subTest(name=name, report=report, status=status):
                    result, env, _ = self.run_step(name, report, status)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(env, "")


if __name__ == "__main__":
    unittest.main()
