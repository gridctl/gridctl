"""Regressions for read-only diagnostic requests and secret-safe output."""

import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch
import urllib.error


SPEC = importlib.util.spec_from_file_location("diagnostic", Path(__file__).with_name("tap-auth-diagnostic.py"))
diagnostic = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(diagnostic)


class TapDiagnosticTests(unittest.TestCase):
    def test_response_categories_never_echo_server_text(self):
        self.assertEqual("bad_credentials", diagnostic.classify_body('{"message":"Bad credentials"}'))
        for body in ('{"message":"reflected-sensitive-value"}', 'invalid', '[]', '{"message":{}}'):
            self.assertEqual("unrecognized_response", diagnostic.classify_body(body))

    def test_python_uses_existing_helper_get_with_unchanged_token(self):
        with patch.object(diagnostic.release, "api", return_value={}) as api:
            self.assertEqual({"status": 200, "result": "success"}, diagnostic.probe_python(" fixture "))
        api.assert_called_once_with("gridctl/homebrew-tap", "contents/Casks/gridctl.rb",
                                    method="GET", token=" fixture ")

    def test_python_error_does_not_expose_credentials(self):
        error = urllib.error.HTTPError("https://api.github.com", 401, "sensitive-value", {},
                                       io.BytesIO(b'{"message":"Bad credentials"}'))
        with patch.object(diagnostic.release, "api", side_effect=error):
            self.assertEqual({"status": 401, "result": "bad_credentials"}, diagnostic.probe_python("fixture"))
        with patch.object(diagnostic.release, "api", side_effect=ValueError("sensitive-value")):
            self.assertEqual("request_failed", diagnostic.probe_python("fixture")["result"])
        with patch.object(diagnostic.release, "api", side_effect=error), \
                patch.object(error, "read", side_effect=OSError("sensitive-value")):
            self.assertEqual({"status": 401, "result": "unreadable_response"}, diagnostic.probe_python("fixture"))

    def test_gh_is_read_only_and_isolates_credentials_and_output(self):
        response = subprocess.CompletedProcess([], 1,
            'HTTP/2.0 401 Unauthorized\r\nServer: GitHub\r\n\r\n{"message":"Bad credentials"}',
            "sensitive-value")
        with patch.dict(os.environ, {"GH_TOKEN": "wrong", "GITHUB_TOKEN": "wrong", "GH_DEBUG": "api"}), \
                patch.object(diagnostic.subprocess, "run", return_value=response) as run:
            result = diagnostic.probe_gh(" fixture ")
        self.assertEqual({"status": 401, "result": "bad_credentials"}, result)
        args, kwargs = run.call_args
        self.assertEqual("GET", args[0][args[0].index("--method") + 1])
        self.assertEqual("repos/gridctl/homebrew-tap/contents/Casks/gridctl.rb", args[0][-1])
        self.assertEqual(" fixture ", kwargs["env"]["GH_TOKEN"])
        self.assertNotIn("GITHUB_TOKEN", kwargs["env"])
        self.assertNotIn("GH_DEBUG", kwargs["env"])
        self.assertNotIn(" fixture ", args[0])

    def test_report_flags_whitespace_without_printing_token(self):
        output = io.StringIO()
        with patch.dict(os.environ, {"GORELEASER_TOKEN": " fixture\n"}), \
                patch.object(diagnostic, "probe_python", return_value={"result": "success"}), \
                patch.object(diagnostic, "probe_gh", return_value={"result": "success"}), \
                contextlib.redirect_stdout(output):
            self.assertEqual(0, diagnostic.main())
        self.assertNotIn("fixture", output.getvalue())
        self.assertEqual({"present": True, "surrounding_whitespace": True, "contains_cr_or_lf": True},
                         json.loads(output.getvalue())["credential"])

    def test_missing_secret_does_not_fall_back_to_other_credentials(self):
        with patch.dict(os.environ, {"GORELEASER_TOKEN": "", "GH_TOKEN": "wrong"}), \
                patch.object(diagnostic, "probe_python") as python, \
                patch.object(diagnostic, "probe_gh") as gh, contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(1, diagnostic.main())
        python.assert_not_called()
        gh.assert_not_called()

    def test_workflow_is_manual_main_only_and_read_only(self):
        import yaml
        workflow = yaml.load(Path(".github/workflows/tap-auth-diagnostic.yaml").read_text(), Loader=yaml.BaseLoader)
        self.assertEqual({"workflow_dispatch"}, set(workflow["on"]))
        self.assertEqual({"contents": "read"}, workflow["permissions"])
        self.assertIn("github.ref == 'refs/heads/main'", workflow["jobs"]["diagnose"]["if"])
        self.assertIn("github.repository == 'gridctl/gridctl'", workflow["jobs"]["diagnose"]["if"])


if __name__ == "__main__":
    unittest.main()
