"""Check that the Go lint gate covers integration-tagged files and gofmt."""

import os
from pathlib import Path
import re
import tempfile
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[1]
TOKEN = re.compile(r"\s*(\|\||&&|!|\(|\)|[A-Za-z0-9_.]+)")


def tokenize(expr):
    tokens = []
    pos = 0
    expr = expr.strip()
    while pos < len(expr):
        match = TOKEN.match(expr, pos)
        if not match:
            raise ValueError(f"cannot parse build expression: {expr!r}")
        tokens.append(match.group(1))
        pos = match.end()
        while pos < len(expr) and expr[pos].isspace():
            pos += 1
    return tokens


def negated_tags(expr):
    """Return the tags that appear under an odd number of negations."""
    tokens = tokenize(expr)
    found = set()
    pos = 0

    def parse_or(negated):
        nonlocal pos
        parse_and(negated)
        while pos < len(tokens) and tokens[pos] == "||":
            pos += 1
            parse_and(negated)

    def parse_and(negated):
        nonlocal pos
        parse_not(negated)
        while pos < len(tokens) and tokens[pos] == "&&":
            pos += 1
            parse_not(negated)

    def parse_not(negated):
        nonlocal pos
        if pos >= len(tokens):
            raise ValueError(f"unexpected end of build expression: {expr!r}")
        token = tokens[pos]
        if token == "!":
            pos += 1
            parse_not(not negated)
        elif token == "(":
            pos += 1
            parse_or(negated)
            if pos >= len(tokens) or tokens[pos] != ")":
                raise ValueError(f"unbalanced build expression: {expr!r}")
            pos += 1
        elif token in ("||", "&&", ")"):
            raise ValueError(f"unexpected {token!r} in build expression: {expr!r}")
        else:
            pos += 1
            if negated:
                found.add(token)

    parse_or(False)
    if pos != len(tokens):
        raise ValueError(f"trailing tokens in build expression: {expr!r}")
    return found


def go_files(root):
    skip = {root / "web" / "node_modules", root / ".git"}
    for directory, dirnames, filenames in os.walk(root):
        current = Path(directory)
        dirnames[:] = [d for d in dirnames if current / d not in skip]
        for name in filenames:
            if name.endswith(".go"):
                yield current / name


def files_negating(root, tag):
    offenders = []
    for path in go_files(root):
        for line in path.read_text(errors="replace").splitlines():
            if line.startswith("//go:build "):
                if tag in negated_tags(line[len("//go:build "):]):
                    offenders.append(path.relative_to(root))
            elif line.startswith("package "):
                break
    return offenders


class LintConfigTests(unittest.TestCase):
    def setUp(self):
        self.config = yaml.safe_load((ROOT / ".golangci.yml").read_text())

    def test_integration_build_tag_is_linted(self):
        self.assertIn("integration", self.config["run"]["build-tags"])

    def test_gofmt_formatter_is_enabled(self):
        self.assertIn("gofmt", self.config["formatters"]["enable"])

    def test_no_go_file_negates_integration(self):
        # One tagged run covers every file only while nothing excludes
        # itself from the integration build.
        self.assertEqual(files_negating(ROOT, "integration"), [])


class NegatedTagTests(unittest.TestCase):
    def test_polarity(self):
        cases = {
            "integration": set(),
            "integration && !windows": {"windows"},
            "!integration": {"integration"},
            "! integration || linux": {"integration"},
            "!(integration && linux)": {"integration", "linux"},
            "!(!integration)": set(),
            "linux && !(darwin || !integration)": {"darwin"},
        }
        for expr, want in cases.items():
            with self.subTest(expr=expr):
                self.assertEqual(negated_tags(expr), want)

    def test_malformed_expression_is_rejected(self):
        for expr in ("integration &&", "(integration", "integration)", "a b"):
            with self.subTest(expr=expr):
                with self.assertRaises(ValueError):
                    negated_tags(expr)

    def test_scan_finds_negated_file_and_skips_node_modules(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "pkg").mkdir()
            (root / "pkg" / "ok.go").write_text(
                "//go:build integration && !windows\n\npackage pkg\n"
            )
            (root / "pkg" / "bad.go").write_text(
                "//go:build !integration\n\npackage pkg\n"
            )
            vendored = root / "web" / "node_modules" / "dep"
            vendored.mkdir(parents=True)
            (vendored / "skip.go").write_text("//go:build !integration\n\npackage dep\n")
            self.assertEqual(
                files_negating(root, "integration"), [Path("pkg/bad.go")]
            )


if __name__ == "__main__":
    unittest.main()
