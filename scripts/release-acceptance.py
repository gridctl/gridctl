"""Hosted release acceptance. Mutations are confined to the named sandbox."""

import copy
from datetime import datetime, timezone
import hashlib
import io
import json
import os
from pathlib import Path
import sys
import subprocess
import tempfile
import time
import urllib.request
import urllib.error
import zipfile

import release


SANDBOX = "gridctl/release-verification-sandbox"
TAP = "gridctl/homebrew-verification-sandbox"


def dispatch():
    repository = os.environ["GITHUB_REPOSITORY"]
    if repository != SANDBOX:
        raise ValueError("acceptance publication is sandbox-only")
    tag = f"v0.0.0-sandbox-{os.environ['GITHUB_RUN_ID']}-{os.environ['GITHUB_RUN_ATTEMPT']}"
    sha = os.environ["GITHUB_SHA"]
    now = datetime.now(timezone.utc).replace(microsecond=0)
    name, email = "Release acceptance", "release-acceptance@example.invalid"
    message = "Signed annotated sandbox release\n"
    payload = (f"object {sha}\ntype commit\ntag {tag}\n"
               f"tagger {name} <{email}> {int(now.timestamp())} +0000\n\n{message}").encode()
    # The disposable signing key proves tag-object behavior, not maintainer identity.
    with tempfile.TemporaryDirectory(prefix="sandbox-tag-") as temporary:
        gpg = ["gpg", "--homedir", temporary, "--batch", "--pinentry-mode", "loopback", "--passphrase", ""]
        subprocess.run([*gpg, "--quick-generate-key", f"{name} <{email}>", "ed25519", "sign", "0"],
                       check=True, capture_output=True, timeout=60)
        signature = subprocess.check_output([*gpg, "--armor", "--detach-sign"], input=payload, timeout=60)
        signed = Path(temporary) / "signature.asc"
        signed.write_bytes(signature)
        unsigned = Path(temporary) / "tag.txt"
        unsigned.write_bytes(payload)
        subprocess.run([*gpg, "--verify", str(signed), str(unsigned)], check=True, timeout=60)
    annotated = release.api(repository, "git/tags", "POST", {
        "tag": tag, "message": message + signature.decode(), "object": sha, "type": "commit",
        "tagger": {"name": name, "email": email, "date": now.isoformat()},
    })
    raw = payload + signature
    expected = hashlib.sha1(b"tag " + str(len(raw)).encode() + b"\0" + raw).hexdigest()
    if annotated["sha"] != expected or annotated["object"] != {"type": "commit", "sha": sha,
            "url": f"https://api.github.com/repos/{repository}/git/commits/{sha}"}:
        raise ValueError("signed annotated tag object differs from verified payload")
    release.api(repository, "git/refs", "POST", {"ref": f"refs/tags/{tag}", "sha": annotated["sha"]})
    release.source(repository, f"refs/tags/{tag}", sha)
    print(f"Signed annotated tag object {annotated['sha']} resolves to source commit {sha}")
    release.api(repository, "actions/workflows/release.yaml/dispatches", "POST", {"ref": tag})
    with Path(os.environ["GITHUB_OUTPUT"]).open("a") as output:
        output.write(f"tag={tag}\n")
    deadline = time.monotonic() + 4200
    run = None
    while time.monotonic() < deadline:
        runs = release.api(repository, f"actions/workflows/release.yaml/runs?event=workflow_dispatch&head_sha={sha}&per_page=100")
        matches = [item for item in runs["workflow_runs"] if item["head_branch"] == tag]
        if len(matches) > 1:
            raise ValueError("ambiguous acceptance release runs")
        if matches:
            run = matches[0]
            if run["status"] == "completed":
                break
        time.sleep(20)
    if not run or run["status"] != "completed":
        raise ValueError("timed out observing the actual release workflow")
    print(f"Release workflow evidence: {run['html_url']}")
    jobs = release.api(repository, f"actions/runs/{run['id']}/jobs?per_page=100")["jobs"]
    for job in jobs:
        print(f"{job['name']}: {job['conclusion']} ({job['started_at']} to {job['completed_at']})")
    if run["conclusion"] != "success" or any(job["conclusion"] != "success" for job in jobs):
        request = urllib.request.Request(f"https://api.github.com/repos/{repository}/actions/runs/{run['id']}/logs",
                                         headers={"Authorization": f"Bearer {os.environ['GITHUB_TOKEN']}"})
        with urllib.request.build_opener(release.DownloadRedirect()).open(request, timeout=120) as response:
            logs = response.read()
        with zipfile.ZipFile(io.BytesIO(logs)) as archive:
            for name in archive.namelist():
                if name.endswith(".txt"):
                    print(name)
                    print("\n".join(archive.read(name).decode(errors="replace").splitlines()[-100:]))
        raise ValueError("actual release workflow did not pass all gates")


def inventory():
    directory = Path(os.environ.get("RELEASE_DIST", "dist"))
    tag = os.environ["RELEASE_REF"].removeprefix("refs/tags/")
    schema = json.loads(Path(os.environ["SPDX_SCHEMA"]).read_text())
    index = json.loads((directory / "release-inventory.json").read_text())
    if index["sourceSHA"] != os.environ["GITHUB_SHA"] or index["tag"] != tag:
        raise ValueError("inventory source mismatch")
    for item in index["archives"] + index["inventories"]:
        if release.digest(directory / item["name"]) != item["sha256"]:
            raise ValueError("inventory digest mismatch")
    for name in [value for value in release.expected_assets(tag) if value.endswith(".spdx.json")]:
        document = json.loads((directory / name).read_text())
        frontend = name == "frontend-build.spdx.json"
        release.validate_inventory(document, frontend, schema)
        for mutation in (None, {}, {"packages": []}, {"spdxVersion": "SPDX-0.0"}):
            bad = None if mutation is None else copy.deepcopy(document)
            if mutation is not None:
                bad.update(mutation)
                if mutation == {}:
                    bad.pop("creationInfo", None)
            try:
                release.validate_inventory(bad, frontend, schema)
            except (ValueError, TypeError, AttributeError):
                pass
            except Exception as error:
                import jsonschema
                if not isinstance(error, jsonschema.ValidationError):
                    raise
            else:
                raise ValueError("malformed or empty inventory was accepted")
    print("Authenticated archive/inventory bindings and malformed/empty inventory rejections passed")


def order():
    tag = os.environ["RELEASE_REF"].removeprefix("refs/tags/")
    record = release.release_record(SANDBOX, tag)
    if record.get("draft") is not False or not record.get("published_at"):
        raise ValueError("release was not published")
    for asset in record["assets"]:
        if asset["updated_at"] > record["published_at"]:
            raise ValueError("an asset changed after publication")
    commits = release.api(TAP, "commits?path=Casks/gridctl.rb&per_page=1")
    commit = commits[0]
    if tag not in commit["commit"]["message"]:
        raise ValueError("tap did not advance to the tested release")
    if commit["commit"]["committer"]["date"] < record["published_at"]:
        raise ValueError("tap advanced before public release")
    print(f"Draft asset assembly preceded publication at {record['published_at']}; tap commit {commit['sha']} followed")


def immutable():
    try:
        settings = release.api(SANDBOX, "immutable-releases")
    except urllib.error.HTTPError as error:
        if error.code not in (403, 404):
            raise
        settings = None
        print(f"Settings preflight returned HTTP {error.code}; Administration read is unavailable")
    if settings is not None and settings.get("enabled") is True:
        release.check_immutable("immutable", settings)
        print("Live immutable settings prerequisite is enabled; this does not test immutable publication")
    else:
        try:
            release.check_immutable("immutable", settings)
        except ValueError as error:
            if "immutable release prerequisite absent" not in str(error):
                raise
            print(f"Live missing-configuration rejection passed: {error}")
        else:
            raise ValueError("immutable mode accepted absent live prerequisites")


if __name__ == "__main__":
    if os.environ["GITHUB_REPOSITORY"] != SANDBOX:
        raise SystemExit("acceptance is sandbox-only")
    {"dispatch": dispatch, "inventory": inventory, "order": order, "immutable": immutable}[sys.argv[1]]()
