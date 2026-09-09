"""Unit fixtures for the real scanner wrapper's policy and failure handling."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


class GovulncheckTests(unittest.TestCase):
    def test_scanner_policy(self):
        config = {"config": {"scanner_name": "govulncheck"}}

        def finding(identifier, reachable=True):
            frame = {"module": "example.com/module"}
            if reachable:
                frame["function"] = "Vulnerable"
            return {"finding": {"osv": identifier, "trace": [frame]}}

        cases = (
            ("clean", [config], 0, 0),
            ("reachable", [config, finding("GO-2099-0001")], 0, 3),
            ("import-only", [config, finding("GO-2099-0001", False)], 0, 0),
            ("approved", [config, finding("GO-2026-4887")], 0, 0),
            ("malformed", "not json", 0, 1),
            ("empty", "", 0, 1),
            ("missing-config", [finding("GO-2099-0001", False)], 0, 1),
            ("malformed-finding", [config, {"finding": {"osv": "GO-2099-0001"}}], 0, 1),
            ("scanner-failure", [config], 7, 1),
        )
        with tempfile.TemporaryDirectory(dir=".", prefix=".scanner-test-") as directory:
            root = Path(directory).resolve()
            executable = root / "govulncheck"
            executable.write_text('#!/bin/sh\ncat "$SCANNER_FIXTURE"\nexit "$SCANNER_EXIT"\n')
            executable.chmod(0o700)
            fixture = root / "fixture.json"
            for name, output, scanner_exit, expected in cases:
                with self.subTest(name=name):
                    fixture.write_text(output if isinstance(output, str) else
                                       "\n".join(json.dumps(item) for item in output))
                    env = dict(os.environ, PATH=f"{root}:{os.environ['PATH']}",
                               SCANNER_FIXTURE=str(fixture), SCANNER_EXIT=str(scanner_exit))
                    result = subprocess.run(["bash", "scripts/govulncheck.sh"], env=env,
                                            capture_output=True, text=True, timeout=10)
                    self.assertEqual(expected, result.returncode, result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
