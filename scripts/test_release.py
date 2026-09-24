"""Unit regressions for release policy; hosted tests cover actual publication."""

import copy
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import subprocess
import unittest
from unittest.mock import patch
import urllib.error
import urllib.request


SPEC = importlib.util.spec_from_file_location("release", Path(__file__).with_name("release.py"))
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)


class ReleasePolicyTests(unittest.TestCase):
    def test_rejection_diagnostic_distinguishes_infrastructure(self):
        diagnostic = "Error: expected SourceRepositoryOwnerURI to be https://github.com/wrong, got https://github.com/gridctl"
        markers = ("expected SourceRepositoryOwnerURI",)
        release.check_rejection(subprocess.CompletedProcess([], 1, "", diagnostic), markers)
        for code, error in ((0, diagnostic), (1, "network timeout"), (4, "authentication required"), (1, "")):
            with self.subTest(code=code, error=error):
                with self.assertRaises(ValueError):
                    release.check_rejection(subprocess.CompletedProcess([], code, "", error), markers)

    def test_workflow_gate_set(self):
        import yaml

        gates = yaml.safe_load(Path(".github/workflows/gatekeeper.yaml").read_text())["jobs"]
        self.assertEqual(set(release.GATES), set(gates["required"]["needs"]))
        self.assertIn("always()", gates["required"]["if"])
        publisher = yaml.safe_load(Path(".github/workflows/release.yaml").read_text())["jobs"]
        self.assertEqual("./.github/workflows/gatekeeper.yaml", publisher["validation"]["uses"])
        self.assertEqual("validation", publisher["assemble"]["needs"])
        self.assertEqual({"validation", "assemble", "verify-linux", "verify-macos"},
                         set(publisher["publish"]["needs"]))
        self.assertEqual("publish", publisher["homebrew"]["needs"])

        assemble_steps = publisher["assemble"]["steps"]
        commands = [step.get("run", "") for step in assemble_steps]
        generate = commands.index("goreleaser release --clean")
        validate = next(index for index, command in enumerate(commands)
                        if "validate_generated_cask.py" in command)
        prepare = commands.index("python3 scripts/release.py prepare")
        attest = next(index for index, step in enumerate(assemble_steps)
                      if step.get("uses", "").startswith("actions/attest@"))
        self.assertLess(generate, validate)
        self.assertLess(validate, prepare)
        self.assertLess(prepare, attest)
        self.assertIn("ruby -c dist/homebrew/Casks/gridctl.rb", commands[validate])

    def test_homebrew_xattr_uses_declarative_steps(self):
        import yaml

        config = yaml.safe_load(Path(".goreleaser.yaml").read_text())
        cask = config["homebrew_casks"][0]
        self.assertTrue(config["release"]["draft"])
        self.assertTrue(cask["skip_upload"])
        self.assertEqual(
            {
                "owner": "gridctl",
                "name": "homebrew-tap",
                "token": "{{ .Env.GORELEASER_TOKEN }}",
            },
            cask["repository"],
        )
        self.assertEqual(
            'postflight_steps do\n'
            '  on_macos do\n'
            '    run "/usr/bin/xattr",\n'
            '        args: ["-dr", "com.apple.quarantine", "{{ "{{staged_path}}" }}/gridctl"]\n'
            '  end\n'
            'end\n',
            cask["custom_block"],
        )
        self.assertNotIn("hooks", cask)

    def test_goreleaser_pin_matches_cask_regression(self):
        tools_spec = importlib.util.spec_from_file_location(
            "release_tools", Path(__file__).with_name("release-tools.py")
        )
        release_tools = importlib.util.module_from_spec(tools_spec)
        tools_spec.loader.exec_module(release_tools)
        regression_spec = importlib.util.spec_from_file_location(
            "check_goreleaser_cask", Path(__file__).with_name("check_goreleaser_cask.py")
        )
        regression = importlib.util.module_from_spec(regression_spec)
        regression_spec.loader.exec_module(regression)
        self.assertEqual(
            "v" + regression.GORELEASER_VERSION, release_tools.TOOLS["goreleaser"][1]
        )

    @unittest.skipUnless(
        importlib.util.find_spec("jsonschema"),
        "jsonschema is installed by the release-policy CI environment",
    )
    def test_prepare_requires_complete_inventories(self):
        import jsonschema

        tag, sha = "v1.2.3", "a" * 40
        schema = {"type": "object", "required": ["spdxVersion", "packages"],
                  "properties": {"packages": {"type": "array"}}}
        with tempfile.TemporaryDirectory(dir=".", prefix=".release-test-") as temporary:
            directory = Path(temporary)
            schema_path = directory / "schema.json"
            schema_path.write_text(json.dumps(schema))
            for name in release.archive_names(tag):
                (directory / name).write_bytes(b"archive fixture")
                (directory / (name + ".spdx.json")).write_text(json.dumps({
                    "spdxVersion": "SPDX-2.3", "packages": [{"name": "github.com/spf13/cobra"}]}))
            inventory = directory / "frontend-build.spdx.json"
            good = json.dumps({"spdxVersion": "SPDX-2.3", "packages": [{"name": "react"}, {"name": "vite"}]})
            inventory.write_text(good)
            cask = directory / "homebrew/Casks/gridctl.rb"
            cask.parent.mkdir(parents=True)
            cask_bytes = b"cask fixture\nwith exact bytes \x00\xff"
            cask.write_bytes(cask_bytes)
            with patch.dict(os.environ, {"GITHUB_REPOSITORY": "owner/repo"}):
                names = release.prepare(directory, tag, sha, schema_path)
                self.assertEqual(set(release.expected_assets(tag)) - {"provenance.sigstore.json"}, set(names))
                prepared = directory / "gridctl.rb"
                self.assertEqual(cask_bytes, prepared.read_bytes())
                checksums = (directory / "checksums.txt").read_text()
                subjects = (directory / "attestation-subjects.txt").read_text()
                cask_digest = release.digest(cask)
                self.assertEqual(cask_digest, release.digest(prepared))
                cask_subject = f"{cask_digest}  gridctl.rb\n"
                self.assertEqual(1, checksums.count(cask_subject))
                self.assertEqual(1, subjects.count(cask_subject))
                self.assertNotIn("provenance.sigstore.json", checksums)
                self.assertNotIn("checksums.txt", checksums)
                self.assertIn("checksums.txt", subjects)
                for value in ("", "not json", "{}", '{"spdxVersion":"SPDX-2.3","packages":[]}'):
                    with self.subTest(value=value):
                        inventory.write_text(value)
                        with self.assertRaises((ValueError, jsonschema.ValidationError)):
                            release.prepare(directory, tag, sha, schema_path)
                inventory.write_text(good)
                (directory / release.archive_names(tag)[0]).unlink()
                with self.assertRaises(ValueError):
                    release.prepare(directory, tag, sha, schema_path)

    def test_draft_lookup_uses_authenticated_listing(self):
        missing = urllib.error.HTTPError("https://api.github.com", 404, "Not Found", {}, None)
        draft = {"tag_name": "v1.2.3", "draft": True, "id": 42}
        with patch.object(release, "api", side_effect=[missing, [draft]]) as api:
            self.assertEqual(draft, release.release_record("owner/repo", "v1.2.3"))
            self.assertEqual("releases?per_page=100&page=1", api.call_args.args[1])
        with patch.object(release, "api", side_effect=[missing, []]):
            with self.assertRaises(urllib.error.HTTPError):
                release.release_record("owner/repo", "v1.2.3")

    def test_download_redirect_drops_credentials(self):
        request = urllib.request.Request("https://api.github.com/asset", headers={"Authorization": "test-only"})
        redirected = release.DownloadRedirect().redirect_request(
            request, None, 302, "Found", {}, "https://release-assets.githubusercontent.com/asset")
        self.assertIsNone(redirected.get_header("Authorization"))

    def test_archive_contract(self):
        self.assertEqual([
            "gridctl_1.2.3_darwin_amd64.tar.gz", "gridctl_1.2.3_darwin_arm64.tar.gz",
            "gridctl_1.2.3_linux_amd64.tar.gz", "gridctl_1.2.3_linux_arm64.tar.gz",
        ], release.archive_names("v1.2.3"))
        names = release.expected_assets("v1.2.3")
        self.assertEqual(len(names), len(set(names)))
        self.assertIn("checksums.txt", names)
        self.assertIn("provenance.sigstore.json", names)

    def test_required_gates(self):
        gates = {name: {"result": "success"} for name in release.GATES}
        release.check_gates(gates)
        for name in release.GATES:
            for result in ("failure", "cancelled", "skipped", "", None):
                with self.subTest(name=name, result=result):
                    bad = copy.deepcopy(gates)
                    bad[name]["result"] = result
                    with self.assertRaises(ValueError):
                        release.check_gates(bad)
            bad = copy.deepcopy(gates)
            del bad[name]
            with self.assertRaises(ValueError):
                release.check_gates(bad)
        for bad in ({}, [], None):
            with self.assertRaises(ValueError):
                release.check_gates(bad)

    def test_source_binding(self):
        sha = "a" * 40
        release.check_source("refs/tags/v1.2.3", sha, sha)
        for ref, expected, actual in (
            ("refs/heads/main", sha, sha),
            ("refs/tags/v1.2.3", sha, "b" * 40),
            ("refs/tags/v1.2.3", "abc", "abc"),
            ("refs/tags/v1.2.3", sha, ""),
        ):
            with self.subTest(ref=ref, expected=expected, actual=actual):
                with self.assertRaises(ValueError):
                    release.check_source(ref, expected, actual)

    def test_checkout_source_peels_tag_objects(self):
        commit, tag_object = "a" * 40, "b" * 40
        environment = {"GITHUB_SHA": tag_object, "GITHUB_REF": "refs/tags/v1.2.3",
                       "RELEASE_SOURCE_SHA": commit}
        with patch.dict(os.environ, environment), patch.object(
                release.subprocess, "check_output", side_effect=[commit + "\n"] * 3) as git:
            self.assertEqual(commit, release.checkout_source())
            self.assertEqual([call.args[0][-1] for call in git.call_args_list],
                             ["HEAD^{commit}", tag_object + "^{commit}", "refs/tags/v1.2.3^{commit}"])
        for values in ([commit, tag_object, commit], [commit, commit, tag_object]):
            with patch.dict(os.environ, environment), patch.object(
                    release.subprocess, "check_output", side_effect=values):
                with self.assertRaisesRegex(ValueError, "checkout/source mismatch"):
                    release.checkout_source()
        with patch.dict(os.environ, {**environment, "RELEASE_SOURCE_SHA": tag_object}), patch.object(
                release.subprocess, "check_output", return_value=commit):
            with self.assertRaisesRegex(ValueError, "validated source mismatch"):
                release.checkout_source()

    def test_immutable_prerequisite(self):
        release.check_immutable("mutable", None)
        release.check_immutable("immutable", {"enabled": True})
        for mode, settings in (("", None), ("invalid", {}), ("immutable", None),
                               ("immutable", {}), ("immutable", {"enabled": False}),
                               ("immutable", {"enabled": "true"})):
            with self.subTest(mode=mode, settings=settings):
                with self.assertRaises(ValueError):
                    release.check_immutable(mode, settings)


if __name__ == "__main__":
    unittest.main()
