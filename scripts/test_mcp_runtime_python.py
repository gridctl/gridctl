"""Unit regressions for the MCP Python runtime image policy."""

import importlib.util
import json
from pathlib import Path
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location(
    "mcp_runtime_python", Path(__file__).with_name("mcp_runtime_python.py"))
policy = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(policy)


class MCPRuntimePythonPolicyTests(unittest.TestCase):
    def test_recipe_and_workflow_policy(self):
        pins = policy.validate_recipe()
        workflow = policy.validate_workflow()
        self.assertEqual("ghcr.io/gridctl/mcp-runtime-python", pins["package"])
        self.assertEqual("3.12", pins["python"]["minor"])
        self.assertEqual(10001, pins["identity"]["uid"])
        self.assertEqual(10001, pins["identity"]["gid"])
        self.assertIn("validate", workflow["jobs"])
        self.assertEqual({}, workflow["permissions"])

    def test_generator_is_not_migrated(self):
        generator = policy.GENERATOR.read_text()
        self.assertIn("python-uv-v1", generator)
        self.assertNotIn("mcp-runtime-python", generator)
        self.assertIn("chown -R gridctl:gridctl", generator)

    def test_revision_and_candidate_tags(self):
        sha = "a" * 40
        self.assertEqual(f"sha-{sha}", policy.revision_tag(sha))
        self.assertEqual(f"candidate-sha-{sha}", policy.candidate_tag(sha))
        with self.assertRaises(ValueError):
            policy.revision_tag("abc")
        policy.assert_immutable_tag(f"sha-{sha}")
        policy.assert_immutable_tag(f"candidate-sha-{sha}")
        with self.assertRaises(ValueError):
            policy.assert_immutable_tag("3.12")
        with self.assertRaises(ValueError):
            policy.assert_immutable_tag("latest")

    def test_refuse_overwrite(self):
        sha = "b" * 40
        tag = policy.revision_tag(sha)
        self.assertEqual(tag, policy.refuse_overwrite([], tag))
        with self.assertRaises(ValueError):
            policy.refuse_overwrite([tag], tag)

    def test_index_and_platform_subjects(self):
        index = "sha256:" + "1" * 64
        amd = "sha256:" + "2" * 64
        arm = "sha256:" + "3" * 64
        inventory = policy.evidence_inventory(
            index,
            {"linux/amd64": amd, "linux/arm64": arm},
            {"index": "index.spdx.json", "linux/amd64": "amd64.spdx.json", "linux/arm64": "arm64.spdx.json"},
        )
        self.assertEqual("index", inventory["index"]["kind"])
        self.assertEqual(["linux/amd64", "linux/arm64"], [item["platform"] for item in inventory["platforms"]])
        with self.assertRaises(ValueError):
            policy.evidence_inventory(index, {"linux/amd64": amd, "linux/arm64": arm}, {"index": "index.spdx.json"})
        with self.assertRaises(ValueError):
            policy.classify_subject("index", "sha256:deadbeef")
        with self.assertRaises(ValueError):
            policy.classify_subject("manifest", index)

    def test_native_versus_emulated(self):
        self.assertEqual("native", policy.native_execution("x86_64", "linux/amd64")["mode"])
        self.assertEqual("native", policy.native_execution("aarch64", "linux/arm64")["mode"])
        self.assertEqual("emulated", policy.native_execution("x86_64", "linux/arm64")["mode"])
        self.assertEqual("unknown", policy.native_execution("ppc64le", "linux/amd64")["mode"])

    def test_source_binding(self):
        sha = "c" * 40
        self.assertEqual(sha, policy.check_source(sha, sha))
        with self.assertRaises(ValueError):
            policy.check_source(sha, "d" * 40)

    def test_supported_aliases_are_not_immutable(self):
        pins = policy.load_pins()
        for alias in pins["tags"]["supported_aliases"]:
            with self.assertRaises(ValueError):
                policy.assert_immutable_tag(alias)

    def test_cli_subjects_and_tags(self):
        sha = "e" * 40
        with patch.object(policy.sys, "argv", ["mcp_runtime_python.py", "revision-tag", sha]):
            with patch.object(policy.sys, "stdout"):
                policy.main(["revision-tag", sha])
        digest = "sha256:" + "4" * 64
        other = "sha256:" + "5" * 64
        inventory = json.loads(self._stdout("subjects", digest, json.dumps({"linux/amd64": other}), json.dumps({"index": "i", "linux/amd64": "p"})))
        self.assertEqual("index", inventory["index"]["kind"])

    def _stdout(self, *argv):
        from io import StringIO
        buffer = StringIO()
        with patch.object(policy.sys, "stdout", buffer):
            policy.main(list(argv))
        return buffer.getvalue()


if __name__ == "__main__":
    unittest.main()
