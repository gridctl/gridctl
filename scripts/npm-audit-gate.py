#!/usr/bin/env python3
"""Gate an `npm audit --json` report at high severity, with dated exceptions.

`npm audit` has no way to accept one advisory, so a high advisory with no
patched release fails every pull request. This gate keeps the same bar
(any high or critical advisory fails) and lets a reviewed exceptions file
accept named advisories until a date.

An exception fails the gate when it has expired, when it expires more than
MAX_WINDOW_DAYS ahead, or when the report no longer contains its advisory at
high severity. The last rule removes an entry as soon as a lockfile update
clears the advisory.

Exit codes:
  0 = no high or critical advisory, or every one is excepted
  1 = unexcepted advisory, or an expired, overlong, or unused exception
  2 = unreadable or malformed report or exceptions file
"""

import argparse
import datetime
import json
import re
import sys

BLOCKING = {"high", "critical"}
NON_BLOCKING = {"info", "low", "moderate"}
MAX_WINDOW_DAYS = 30
ADVISORY_ID = re.compile(r"GHSA(?:-[0-9a-z]{4}){3}")
ADVISORY_URL = re.compile(r"https://github\.com/advisories/(" + ADVISORY_ID.pattern + ")")
EXCEPTION_KEYS = {"id", "package", "reason", "expires"}


class InputError(Exception):
    """The report or the exceptions file cannot be trusted."""


def load_json(path, label):
    try:
        with open(path, encoding="utf-8") as handle:
            return json.load(handle)
    except (OSError, ValueError) as err:
        raise InputError(f"{label} {path}: {err}") from err


def advisories_by_package(report):
    """Return {package: {advisory id: (severity, package named by the advisory)}}."""
    if not isinstance(report, dict) or report.get("error") is not None:
        raise InputError("npm audit report carries an error or is not an object")
    total = report.get("metadata", {}).get("vulnerabilities", {}).get("total")
    vulnerabilities = report.get("vulnerabilities")
    if isinstance(total, bool) or not isinstance(total, int) or not isinstance(vulnerabilities, dict):
        raise InputError("npm audit report has no vulnerability counts")

    direct = {}
    for name, entry in vulnerabilities.items():
        severity = entry.get("severity") if isinstance(entry, dict) else None
        if severity not in BLOCKING | NON_BLOCKING:
            raise InputError(f"package {name} has unknown severity {severity!r}")
        via = entry.get("via")
        if not isinstance(via, list):
            raise InputError(f"package {name} has no via list")
        found = {}
        for item in via:
            if isinstance(item, str):
                continue
            if not isinstance(item, dict) or item.get("severity") not in BLOCKING | NON_BLOCKING:
                raise InputError(f"package {name} has a malformed advisory")
            match = ADVISORY_URL.fullmatch(str(item.get("url", "")))
            if item["severity"] in BLOCKING and not match:
                raise InputError(f"package {name} has a {item['severity']} advisory with no GHSA URL")
            if match:
                found[match.group(1)] = (item["severity"], str(item.get("name", name)))
        direct[name] = found
    return direct, vulnerabilities


def blocking_advisories(report):
    """Return {advisory id: package} for every high or critical advisory.

    A package npm rates high or critical must trace to at least one such
    advisory through its via chain; one that does not fails closed.
    """
    direct, vulnerabilities = advisories_by_package(report)
    blocking = {}
    for name, entry in vulnerabilities.items():
        seen, stack, reached = set(), [name], {}
        while stack:
            current = stack.pop()
            if current in seen or current not in vulnerabilities:
                continue
            seen.add(current)
            reached.update(direct[current])
            stack.extend(item for item in vulnerabilities[current]["via"] if isinstance(item, str))
        high = {gid: pkg for gid, (severity, pkg) in reached.items() if severity in BLOCKING}
        if entry["severity"] in BLOCKING and not high:
            raise InputError(f"package {name} is {entry['severity']} with no advisory to attribute it to")
        blocking.update(high)
    return blocking


def load_exceptions(path):
    document = load_json(path, "exceptions file")
    if not isinstance(document, dict) or set(document) != {"schema_version", "exceptions"}:
        raise InputError("exceptions file must hold exactly schema_version and exceptions")
    if document["schema_version"] != 1 or not isinstance(document["exceptions"], list):
        raise InputError("exceptions file must be schema_version 1 with an exceptions list")
    entries = {}
    for entry in document["exceptions"]:
        if not isinstance(entry, dict) or set(entry) != EXCEPTION_KEYS:
            raise InputError(f"each exception needs exactly {sorted(EXCEPTION_KEYS)}")
        if not all(isinstance(entry[key], str) and entry[key].strip() for key in EXCEPTION_KEYS):
            raise InputError("exception fields must be non-empty strings")
        if not ADVISORY_ID.fullmatch(entry["id"]):
            raise InputError(f"exception id {entry['id']!r} is not a GHSA identifier")
        if entry["id"] in entries:
            raise InputError(f"exception {entry['id']} is listed twice")
        try:
            expires = datetime.date.fromisoformat(entry["expires"])
        except ValueError as err:
            raise InputError(f"exception {entry['id']} expires on an invalid date") from err
        entries[entry["id"]] = {**entry, "expires": expires}
    return entries


def evaluate(blocking, exceptions, today):
    """Return (failures, accepted) as lists of messages."""
    failures, accepted = [], []
    for gid, package in sorted(blocking.items()):
        entry = exceptions.get(gid)
        if entry is None:
            failures.append(f"{gid} ({package}): high or critical advisory with no exception")
        elif entry["package"] != package:
            failures.append(f"{gid}: exception names {entry['package']}, the advisory is for {package}")
        elif entry["expires"] < today:
            failures.append(f"{gid} ({package}): exception expired on {entry['expires']}")
        elif (entry["expires"] - today).days > MAX_WINDOW_DAYS:
            failures.append(
                f"{gid} ({package}): exception expires more than {MAX_WINDOW_DAYS} days ahead"
            )
        else:
            accepted.append(f"{gid} ({package}): accepted until {entry['expires']}: {entry['reason']}")
    for gid, entry in sorted(exceptions.items()):
        if gid not in blocking:
            failures.append(
                f"{gid} ({entry['package']}): exception is unused; the report no longer has "
                "this advisory at high severity, so remove the entry"
            )
    return failures, accepted


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--report", required=True, help="file holding `npm audit --json` output")
    parser.add_argument("--exceptions", required=True, help="exceptions file")
    parser.add_argument("--today", help="override the current UTC date (YYYY-MM-DD), for tests")
    args = parser.parse_args(argv)
    try:
        today = (
            datetime.date.fromisoformat(args.today)
            if args.today
            else datetime.datetime.now(datetime.timezone.utc).date()
        )
        blocking = blocking_advisories(load_json(args.report, "report"))
        exceptions = load_exceptions(args.exceptions)
    except (InputError, ValueError) as err:
        print(f"npm-audit-gate: {err}", file=sys.stderr)
        return 2
    failures, accepted = evaluate(blocking, exceptions, today)
    for line in accepted:
        print(f"npm-audit-gate: {line}")
    for line in failures:
        print(f"npm-audit-gate: FAIL {line}", file=sys.stderr)
    if failures:
        return 1
    print(f"npm-audit-gate: pass ({len(accepted)} excepted, 0 unexcepted high or critical)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
