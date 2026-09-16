"""Unit regressions for the MCP Python runtime image policy."""

import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location(
    "mcp_runtime_python", Path(__file__).with_name("mcp_runtime_python.py"))
policy = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(policy)


def _digest(n):
    return "sha256:" + str(n) * 64


def _sbom(path, subject, digest="a" * 64):
    return {"path": path, "sha256": digest, "subject": subject}


def _dsse(payload):
    return {"bundle": {"dsseEnvelope": {
        "payload": __import__("base64").b64encode(json.dumps(payload).encode()).decode()}}}


def _statement(predicate_type, subjects, source_sha):
    return {
        "predicateType": predicate_type,
        "subject": [{"digest": {"sha256": digest[7:]}} for digest in subjects],
        "predicate": {"workflow": policy.WORKFLOW_PATH, "source_sha": source_sha},
    }


class MCPRuntimePythonPolicyTests(unittest.TestCase):
    def test_recipe_and_workflow_policy(self):
        pins = policy.validate_recipe()
        workflow = policy.validate_workflow()
        self.assertEqual("ghcr.io/gridctl/mcp-runtime-python", pins["package"])
        self.assertEqual("3.12", pins["python"]["minor"])
        self.assertEqual(10001, pins["identity"]["uid"])
        self.assertEqual(10001, pins["identity"]["gid"])
        self.assertEqual("0.19.0-1", pins["init"]["version"])
        self.assertTrue(pins["init"]["debian_snapshot"])
        self.assertIn("validate", workflow["jobs"])
        self.assertEqual({}, workflow["permissions"])
        self.assertNotIn("test-amd64", workflow["jobs"]["promote-supported"].get("needs") or [])

    def test_changing_reviewed_tini_pin_requires_recipe_revision(self):
        original = policy.PINS.read_text()
        try:
            policy.PINS.write_text(original.replace("0.19.0-1", "0.0.0-0"))
            with self.assertRaises(ValueError):
                policy.validate_recipe()
        finally:
            policy.PINS.write_text(original)

    def test_changing_setuptools_pin_requires_recipe_revision(self):
        original = policy.PINS.read_text()
        try:
            policy.PINS.write_text(original.replace("78.1.1", "0.0.0"))
            with self.assertRaises(ValueError):
                policy.validate_recipe()
        finally:
            policy.PINS.write_text(original)

    def test_virtual_lockfile_is_rejected(self):
        original = policy.LOCKFILE.read_text()
        try:
            policy.LOCKFILE.write_text(original.replace(
                'source = { editable = "." }', 'source = { virtual = "." }'))
            with self.assertRaises(ValueError):
                policy.validate_recipe()
        finally:
            policy.LOCKFILE.write_text(original)

    def test_lockfile_revision_must_remain_1(self):
        original = policy.LOCKFILE.read_text()
        try:
            policy.LOCKFILE.write_text(original.replace("revision = 1", "revision = 3"))
            with self.assertRaises(ValueError):
                policy.validate_recipe()
        finally:
            policy.LOCKFILE.write_text(original)

    def test_generator_is_not_migrated(self):
        generator = policy.GENERATOR.read_text()
        self.assertIn("python-uv-v1", generator)
        self.assertNotIn("mcp-runtime-python", generator)
        self.assertIn("chown -R gridctl:gridctl", generator)

    def test_revision_and_candidate_tags(self):
        sha = "a" * 40
        self.assertEqual(f"sha-{sha}", policy.revision_tag(sha))
        self.assertEqual(f"candidate-sha-{sha}", policy.candidate_tag(sha))
        self.assertEqual(f"sha-{sha}-amd64", policy.platform_tag(sha, "amd64"))
        with self.assertRaises(ValueError):
            policy.revision_tag("abc")
        policy.assert_immutable_tag(f"sha-{sha}")
        policy.assert_immutable_tag(f"candidate-sha-{sha}")
        policy.assert_immutable_tag(f"sha-{sha}-amd64")
        policy.assert_immutable_tag(f"sha-{sha}-arm64")
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

    def test_require_absent_fail_closed(self):
        sha = "b" * 40
        tag = policy.revision_tag(sha)
        self.assertEqual(tag, policy.require_absent(1, "manifest unknown: ghcr.io/gridctl/mcp-runtime-python", tag))
        self.assertEqual(
            policy.platform_tag(sha, "amd64"),
            policy.require_absent(1, "no such manifest: sha-...-amd64", policy.platform_tag(sha, "amd64")))
        with self.assertRaises(ValueError):
            policy.require_absent(0, json.dumps({"schemaVersion": 2}), tag)
        with self.assertRaises(ValueError):
            policy.require_absent(1, "unauthorized: authentication required", tag)
        with self.assertRaises(ValueError):
            policy.require_absent(1, "Get https://ghcr.io/v2/: net/http: TLS handshake timeout", tag)
        with self.assertRaises(ValueError):
            policy.require_absent(1, "toomanyrequests: You have reached your unauthenticated pull rate limit", tag)

    def test_index_and_platform_subjects(self):
        index = _digest(1)
        amd = _digest(2)
        arm = _digest(3)
        inventory = policy.evidence_inventory(
            index,
            {"linux/amd64": amd, "linux/arm64": arm},
            {
                "index": _sbom("index.spdx.json", index),
                "linux/amd64": _sbom("amd64.spdx.json", amd, "b" * 64),
                "linux/arm64": _sbom("arm64.spdx.json", arm, "c" * 64),
            },
        )
        self.assertEqual("index", inventory["index"]["kind"])
        self.assertEqual(["linux/amd64", "linux/arm64"], [item["platform"] for item in inventory["platforms"]])
        with self.assertRaises(ValueError):
            policy.evidence_inventory(index, {"linux/amd64": amd, "linux/arm64": arm}, {"index": _sbom("index.spdx.json", index)})
        with self.assertRaises(ValueError):
            policy.evidence_inventory(
                index,
                {"linux/amd64": amd, "linux/arm64": arm},
                {
                    "index": _sbom("index.spdx.json", index),
                    "linux/amd64": _sbom("amd64.spdx.json", arm),
                    "linux/arm64": _sbom("arm64.spdx.json", arm, "c" * 64),
                },
            )
        with self.assertRaises(ValueError):
            policy.evidence_inventory(
                index,
                {"linux/amd64": amd, "linux/arm64": arm},
                {
                    "index": "index.spdx.json",
                    "linux/amd64": "amd64.spdx.json",
                    "linux/arm64": "arm64.spdx.json",
                },
            )
        with self.assertRaises(ValueError):
            policy.classify_subject("index", "sha256:deadbeef")
        with self.assertRaises(ValueError):
            policy.classify_subject("manifest", index)

    def test_bind_evidence_hashes_sboms(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            index, amd, arm = _digest(1), _digest(2), _digest(3)
            files = {
                "index": directory / "index.spdx.json",
                "linux/amd64": directory / "amd64.spdx.json",
                "linux/arm64": directory / "arm64.spdx.json",
            }
            for path in files.values():
                path.write_text('{"spdxVersion":"SPDX-2.3"}\n')
            inventory = policy.bind_evidence(
                index,
                {"linux/amd64": amd, "linux/arm64": arm},
                {name: str(path) for name, path in files.items()},
                "d" * 40,
            )
            self.assertEqual("passed", inventory["scan_status"])
            self.assertEqual(policy.file_sha256(files["index"]), inventory["sboms"]["index"]["sha256"])
            with self.assertRaises(ValueError):
                policy.bind_evidence(index, {"linux/amd64": amd, "linux/arm64": arm},
                                     {name: str(path) for name, path in files.items()},
                                     "d" * 40, scan_status="failed")

    def test_promotion_rejects_failed_scan_wrong_sha_and_missing_evidence(self):
        index, amd, arm = _digest(4), _digest(5), _digest(6)
        sha = "e" * 40
        ok = {
            "candidate_sha": sha,
            "attested_source_sha": sha,
            "candidate_digest": index,
            "index_digest": index,
            "platforms": {"linux/amd64": amd, "linux/arm64": arm},
            "sbom_subjects": [index, amd, arm],
            "scan_status": "passed",
            "public_evidence": True,
            "workflow": "https://github.com/gridctl/gridctl/.github/workflows/mcp-runtime-python.yaml@refs/heads/main",
        }
        policy.assert_promotable(ok)
        failed = dict(ok, scan_status="failed")
        with self.assertRaises(ValueError):
            policy.assert_promotable(failed)
        with self.assertRaises(ValueError):
            policy.assert_promotable(dict(ok, attested_source_sha="f" * 40))
        with self.assertRaises(ValueError):
            policy.assert_promotable(dict(ok, public_evidence=False))
        with self.assertRaises(ValueError):
            policy.assert_promotable(dict(ok, sbom_subjects=[index, amd]))

    def test_public_evidence_rejects_missing_or_wrong_scope_sboms(self):
        index, amd, arm = _digest(7), _digest(8), _digest(9)
        expected = {
            "index_digest": index,
            "platforms": {"linux/amd64": amd, "linux/arm64": arm},
            "repository": "gridctl/gridctl",
            "source_sha": "a" * 40,
        }
        ok = {
            "anonymous": True,
            "image_digest": index,
            "repository": "gridctl/gridctl",
            "workflow": policy.WORKFLOW_PATH,
            "source_sha": "a" * 40,
            "attested_subjects": [index, amd, arm],
            "sbom_subjects": [index, amd, arm],
            "scan_status": "passed",
            "predicate_types": [policy.SLSA_PREDICATE, policy.SPDX_PREDICATE, policy.EVIDENCE_PREDICATE],
        }
        self.assertEqual(index, policy.verify_public_evidence(ok, expected))
        with self.assertRaises(ValueError):
            policy.verify_public_evidence(dict(ok, anonymous=False), expected)
        with self.assertRaises(ValueError):
            policy.verify_public_evidence(dict(ok, sbom_subjects=[index, amd]), expected)
        with self.assertRaises(ValueError):
            policy.verify_public_evidence(dict(ok, sbom_subjects=[index, amd, _digest(0)]), expected)
        with self.assertRaises(ValueError):
            policy.verify_public_evidence(dict(ok, predicate_types=[policy.SLSA_PREDICATE]), expected)
        with self.assertRaises(ValueError):
            policy.verify_public_evidence(dict(ok, scan_status="failed"), expected)
        with self.assertRaises(ValueError):
            policy.verify_public_evidence(dict(ok, scan_status=""), expected)

    def test_native_versus_emulated(self):
        self.assertEqual("native", policy.native_execution("x86_64", "linux/amd64")["mode"])
        self.assertEqual("native", policy.native_execution("aarch64", "linux/arm64")["mode"])
        self.assertEqual("emulated", policy.native_execution("x86_64", "linux/arm64")["mode"])
        self.assertEqual("unknown", policy.native_execution("ppc64le", "linux/amd64")["mode"])
        with self.assertRaises(ValueError):
            policy.require_native("x86_64", "linux/arm64")
        policy.require_native("x86_64", "linux/amd64")

    def test_require_executed(self):
        passed = "\n".join([
            json.dumps({"Action": "run", "Test": "TestMCPRuntimePython"}),
            json.dumps({"Action": "pass", "Test": "TestMCPRuntimePython"}),
        ])
        self.assertEqual("TestMCPRuntimePython", policy.require_executed(passed, "TestMCPRuntimePython"))
        skipped = json.dumps({"Action": "skip", "Test": "TestMCPRuntimePython"})
        with self.assertRaises(ValueError):
            policy.require_executed(skipped, "TestMCPRuntimePython")
        with self.assertRaises(ValueError):
            policy.require_executed("", "TestMCPRuntimePython")

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
        digest = _digest(4)
        other = _digest(5)
        inventory = json.loads(self._stdout(
            "subjects", digest,
            json.dumps({"linux/amd64": other}),
            json.dumps({"index": _sbom("i.spdx.json", digest), "linux/amd64": _sbom("p.spdx.json", other, "b" * 64)})))
        self.assertEqual("index", inventory["index"]["kind"])

    def test_summarize_github_attestations(self):
        index, amd = _digest(1), _digest(2)
        payload = {
            "predicateType": policy.SPDX_PREDICATE,
            "subject": [{"digest": {"sha256": index[7:]}}],
            "predicate": {"workflow": policy.WORKFLOW_PATH, "source": "a" * 40},
        }
        api = {"attestations": [_dsse(payload)]}
        summary = policy.summarize_github_attestations(api, "a" * 40)
        self.assertEqual([index], summary["sbom_subjects"])
        self.assertEqual(policy.WORKFLOW_PATH, summary["workflow"])
        self.assertEqual("a" * 40, summary["source_sha"])
        self.assertEqual("", summary["scan_status"])
        missing = dict(payload, predicate={"workflow": "other.yaml"})
        self.assertEqual("", policy.summarize_github_attestations({"attestations": [_dsse(missing)]}, "a" * 40)["workflow"])
        evidence = {
            "predicateType": policy.EVIDENCE_PREDICATE,
            "subject": [{"digest": {"sha256": index[7:]}}],
            "predicate": {"scan_status": "passed", "workflow": policy.WORKFLOW_PATH, "source_sha": "a" * 40},
        }
        self.assertEqual("passed", policy.summarize_github_attestations({"attestations": [_dsse(evidence)]}, "a" * 40)["scan_status"])
        _ = amd

    def test_verify_candidate_uses_attested_evidence(self):
        index, amd, arm = _digest(1), _digest(2), _digest(3)
        sha = "a" * 40
        subjects = {"index": index, "platforms": {"linux/amd64": amd, "linux/arm64": arm}}
        api = {"attestations": [
            _dsse(_statement(policy.SLSA_PREDICATE, [index, amd, arm], sha)),
            _dsse(_statement(policy.SPDX_PREDICATE, [index], sha)),
            _dsse(_statement(policy.SPDX_PREDICATE, [amd], sha)),
            _dsse(_statement(policy.SPDX_PREDICATE, [arm], sha)),
            _dsse({
                "predicateType": policy.EVIDENCE_PREDICATE,
                "subject": [{"digest": {"sha256": index[7:]}}],
                "predicate": {"scan_status": "passed", "workflow": policy.WORKFLOW_PATH, "source_sha": sha},
            }),
        ]}
        policy.verify_candidate(subjects, api, sha)
        failed = {"attestations": [
            _dsse(_statement(policy.SLSA_PREDICATE, [index, amd, arm], sha)),
            _dsse(_statement(policy.SPDX_PREDICATE, [index], sha)),
            _dsse(_statement(policy.SPDX_PREDICATE, [amd], sha)),
            _dsse(_statement(policy.SPDX_PREDICATE, [arm], sha)),
            _dsse({
                "predicateType": policy.EVIDENCE_PREDICATE,
                "subject": [{"digest": {"sha256": index[7:]}}],
                "predicate": {"scan_status": "failed", "workflow": policy.WORKFLOW_PATH, "source_sha": sha},
            }),
        ]}
        with self.assertRaises(ValueError):
            policy.verify_candidate(subjects, failed, sha)
        with self.assertRaises(ValueError):
            policy.verify_candidate(subjects, api, "b" * 40)
        missing_sbom = {"attestations": [
            _dsse(_statement(policy.SLSA_PREDICATE, [index, amd, arm], sha)),
            _dsse(_statement(policy.SPDX_PREDICATE, [index], sha)),
            _dsse({
                "predicateType": policy.EVIDENCE_PREDICATE,
                "subject": [{"digest": {"sha256": index[7:]}}],
                "predicate": {"scan_status": "passed", "workflow": policy.WORKFLOW_PATH, "source_sha": sha},
            }),
        ]}
        with self.assertRaises(ValueError):
            policy.verify_candidate(subjects, missing_sbom, sha)

    def test_parse_index_subjects(self):
        index, amd, arm = _digest(1), _digest(2), _digest(3)
        parsed = policy.parse_index_subjects({
            "digest": index,
            "manifests": [
                {"digest": amd, "platform": {"os": "linux", "architecture": "amd64"}},
                {"digest": arm, "platform": {"os": "linux", "architecture": "arm64"}},
            ],
        })
        self.assertEqual(index, parsed["index"])
        self.assertEqual(amd, parsed["platforms"]["linux/amd64"])
        self.assertEqual(arm, parsed["platforms"]["linux/arm64"])
        nested = policy.parse_index_subjects({
            "descriptor": {"digest": index},
            "manifest": {
                "manifests": [
                    {"digest": amd, "platform": {"os": "linux", "architecture": "amd64"}},
                    {"digest": arm, "platform": {"os": "linux", "architecture": "arm64"}},
                ],
            },
        })
        self.assertEqual(index, nested["index"])

    def test_fetch_attestations_rejects_invalid_digest(self):
        with self.assertRaises(ValueError):
            policy.fetch_github_attestations(["sha256:deadbeef"])

    def _stdout(self, *argv):
        from io import StringIO
        buffer = StringIO()
        with patch.object(policy.sys, "stdout", buffer):
            policy.main(list(argv))
        return buffer.getvalue()


if __name__ == "__main__":
    unittest.main()
