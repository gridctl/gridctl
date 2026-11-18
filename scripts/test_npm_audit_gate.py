"""Check the npm audit gate against report and exception fixtures."""

import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts/npm-audit-gate.py"
PICOMATCH = "GHSA-fc5q-39xc-vjpf"
OTHER = "GHSA-aaaa-bbbb-cccc"


def advisory(gid, name, severity):
    return {
        "name": name,
        "severity": severity,
        "url": f"https://github.com/advisories/{gid}",
    }


def report(vulnerabilities):
    return {
        "auditReportVersion": 2,
        "vulnerabilities": vulnerabilities,
        "metadata": {"vulnerabilities": {"total": len(vulnerabilities)}},
    }


def transitive_high():
    return report({
        "picomatch": {"severity": "high", "via": [advisory(PICOMATCH, "picomatch", "high")]},
        "tinyglobby": {"severity": "high", "via": ["picomatch"]},
        "vite": {"severity": "high", "via": ["picomatch", "tinyglobby"]},
        "katex": {"severity": "low", "via": [advisory("GHSA-238p-pmpm-9mq7", "katex", "low")]},
    })


def exception(gid=PICOMATCH, package="picomatch", expires="2026-12-02"):
    return {"id": gid, "package": package, "reason": "no patched release", "expires": expires}


class AuditGateTests(unittest.TestCase):
    def gate(self, audit, exceptions, today="2026-11-18", raw_exceptions=None):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            (path / "report.json").write_text(audit if isinstance(audit, str) else json.dumps(audit))
            document = {"schema_version": 1, "exceptions": exceptions}
            (path / "exceptions.json").write_text(
                raw_exceptions if raw_exceptions is not None else json.dumps(document)
            )
            return subprocess.run(
                [
                    sys.executable, str(SCRIPT),
                    "--report", str(path / "report.json"),
                    "--exceptions", str(path / "exceptions.json"),
                    "--today", today,
                ],
                capture_output=True, text=True, check=False,
            )

    def test_clean_report_passes_without_exceptions(self):
        result = self.gate(report({}), [])
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_low_and_moderate_do_not_block(self):
        audit = report({
            "katex": {"severity": "low", "via": [advisory(OTHER, "katex", "low")]},
            "minimatch": {"severity": "moderate", "via": [advisory("GHSA-g3hv-75wg-m249", "minimatch", "moderate")]},
        })
        self.assertEqual(self.gate(audit, []).returncode, 0)

    def test_high_without_exception_fails(self):
        result = self.gate(transitive_high(), [])
        self.assertEqual(result.returncode, 1)
        self.assertIn(f"{PICOMATCH} (picomatch)", result.stderr)

    def test_excepted_advisory_covers_its_dependents(self):
        result = self.gate(transitive_high(), [exception()])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("accepted until 2026-12-02", result.stdout)

    def test_second_high_advisory_still_fails(self):
        audit = transitive_high()
        audit["vulnerabilities"]["vite"]["via"].append(advisory(OTHER, "vite", "critical"))
        result = self.gate(audit, [exception()])
        self.assertEqual(result.returncode, 1)
        self.assertIn(f"{OTHER} (vite)", result.stderr)
        self.assertNotIn(f"FAIL {PICOMATCH}", result.stderr)

    def test_expiry_day_passes_and_next_day_fails(self):
        self.assertEqual(self.gate(transitive_high(), [exception()], today="2026-12-02").returncode, 0)
        result = self.gate(transitive_high(), [exception()], today="2026-12-03")
        self.assertEqual(result.returncode, 1)
        self.assertIn("expired on 2026-12-02", result.stderr)

    def test_window_longer_than_thirty_days_fails(self):
        self.assertEqual(self.gate(transitive_high(), [exception(expires="2026-12-18")]).returncode, 0)
        result = self.gate(transitive_high(), [exception(expires="2026-12-19")])
        self.assertEqual(result.returncode, 1)
        self.assertIn("more than 30 days", result.stderr)

    def test_unused_exception_fails(self):
        result = self.gate(report({}), [exception()])
        self.assertEqual(result.returncode, 1)
        self.assertIn("unused", result.stderr)

    def test_exception_for_a_downgraded_advisory_is_unused(self):
        audit = report({
            "picomatch": {"severity": "moderate", "via": [advisory(PICOMATCH, "picomatch", "moderate")]},
        })
        self.assertEqual(self.gate(audit, [exception()]).returncode, 1)

    def test_package_mismatch_fails(self):
        result = self.gate(transitive_high(), [exception(package="vite")])
        self.assertEqual(result.returncode, 1)
        self.assertIn("the advisory is for picomatch", result.stderr)

    def test_high_package_with_no_advisory_fails_closed(self):
        audit = report({"vite": {"severity": "high", "via": ["missing"]}})
        result = self.gate(audit, [])
        self.assertEqual(result.returncode, 2)
        self.assertIn("no advisory to attribute", result.stderr)

    def test_via_cycle_terminates(self):
        audit = report({
            "a": {"severity": "high", "via": ["b", advisory(PICOMATCH, "picomatch", "high")]},
            "b": {"severity": "high", "via": ["a"]},
        })
        self.assertEqual(self.gate(audit, [exception()]).returncode, 0)

    def test_malformed_reports_exit_two(self):
        unknown = report({"x": {"severity": "severe", "via": []}})
        no_url = report({"x": {"severity": "high", "via": [{"name": "x", "severity": "high"}]}})
        errored = {"error": {"code": "ENOAUDIT"}}
        for audit in ("not json", errored, {"vulnerabilities": {}}, unknown, no_url):
            with self.subTest(audit=audit):
                self.assertEqual(self.gate(audit, []).returncode, 2)

    def test_malformed_exceptions_exit_two(self):
        extra = {**exception(), "note": "x"}
        bad = [
            [extra],
            [exception(gid="CVE-2026-1")],
            [exception(expires="soon")],
            [exception(), exception()],
            [{**exception(), "reason": " "}],
        ]
        for entries in bad:
            with self.subTest(entries=entries):
                self.assertEqual(self.gate(transitive_high(), entries).returncode, 2)
        for raw in ("[]", '{"schema_version": 2, "exceptions": []}', "not json"):
            with self.subTest(raw=raw):
                self.assertEqual(self.gate(transitive_high(), [], raw_exceptions=raw).returncode, 2)


class AuditGateWiringTests(unittest.TestCase):
    def test_committed_exceptions_file_is_valid(self):
        spec = importlib.util.spec_from_file_location("npm_audit_gate", SCRIPT)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        entries = module.load_exceptions(ROOT / "web/audit-exceptions.json")
        for entry in entries.values():
            self.assertGreater(len(entry["reason"]), 20)

    def test_gatekeeper_runs_the_gate_and_fails_on_npm_errors(self):
        workflow = yaml.safe_load((ROOT / ".github/workflows/gatekeeper.yaml").read_text())
        step = next(
            step for step in workflow["jobs"]["frontend"]["steps"]
            if step.get("name") == "Security audit"
        )
        self.assertEqual(step["working-directory"], "web")
        self.assertNotIn("continue-on-error", step)
        run = step["run"]
        self.assertIn("npm audit --json", run)
        self.assertIn("status > 1", run)
        self.assertIn("../scripts/npm-audit-gate.py", run)
        self.assertIn("--exceptions audit-exceptions.json", run)
        self.assertNotIn("--today", run)


if __name__ == "__main__":
    unittest.main()
