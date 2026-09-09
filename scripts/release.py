"""Fail-closed policy shared by release workflows and acceptance tests."""

import base64
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import urllib.error
import urllib.parse
import urllib.request


GATES = ("test", "integration", "litellm-contract", "conformance",
         "podman-integration", "frontend")
PREDICATE = "https://slsa.dev/provenance/v1"


class DownloadRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, message, headers, newurl):
        redirected = super().redirect_request(request, fp, code, message, headers, newurl)
        if urllib.parse.urlsplit(request.full_url).netloc != urllib.parse.urlsplit(newurl).netloc:
            redirected.remove_header("Authorization")
        return redirected


def check_gates(gates):
    if not isinstance(gates, dict):
        raise ValueError("required gate results are missing or malformed")
    for name in GATES:
        gate = gates.get(name)
        if not isinstance(gate, dict) or gate.get("result") != "success":
            raise ValueError(f"required gate {name} did not succeed")


def check_source(ref, expected, actual):
    if not re.fullmatch(r"refs/tags/v[0-9][A-Za-z0-9.+-]*", ref):
        raise ValueError("release source must be a version tag")
    if not re.fullmatch(r"[0-9a-f]{40}", expected) or actual != expected:
        raise ValueError("release tag/source mismatch; refuse publication")


def check_immutable(mode, settings):
    if mode == "mutable":
        return
    if mode != "immutable":
        raise ValueError("release mode must explicitly be mutable or immutable")
    if not isinstance(settings, dict) or settings.get("enabled") is not True:
        raise ValueError("immutable release prerequisite absent: a maintainer must enable "
                         "immutable releases and provide permission to read their settings")


def digest(path):
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def archive_names(tag):
    check_source(f"refs/tags/{tag}", "a" * 40, "a" * 40)
    return [f"gridctl_{tag[1:]}_{system}_{arch}.tar.gz"
            for system in ("darwin", "linux") for arch in ("amd64", "arm64")]


def validate_inventory(document, frontend, schema):
    import jsonschema

    jsonschema.Draft7Validator(schema).validate(document)
    if document.get("spdxVersion") != "SPDX-2.3":
        raise ValueError("inventory must use SPDX-2.3")
    packages = document.get("packages", [])
    names = {item.get("name") for item in packages}
    required = {"react", "vite"} if frontend else {"github.com/spf13/cobra"}
    if not required <= names:
        raise ValueError(f"inventory missing representative components: {sorted(required - names)}")


def prepare(directory, tag, sha, schema_path):
    check_source(f"refs/tags/{tag}", sha, sha)
    names = archive_names(tag)
    schema = json.loads(Path(schema_path).read_text())
    inventories = [name + ".spdx.json" for name in names] + ["frontend-build.spdx.json"]
    for name in names + inventories:
        path = directory / name
        if path.is_symlink() or not path.is_file() or path.stat().st_size == 0:
            raise ValueError(f"missing, empty, or symlinked release asset: {name}")
    for name in inventories:
        validate_inventory(json.loads((directory / name).read_text()),
                           name == "frontend-build.spdx.json", schema)
    index = {
        "schemaVersion": 1, "tag": tag, "sourceSHA": sha,
        "repository": os.environ["GITHUB_REPOSITORY"],
        "inventories": [
            {"name": name, "sha256": digest(directory / name),
             "scope": "frontend build dependency tree, including development tools; not browser-bundle attribution"
             if name == "frontend-build.spdx.json" else "Go components cataloged from the final platform archive",
             "generator": "npm 11.10.1" if name == "frontend-build.spdx.json" else "Syft 1.42.0",
             "artifact": None if name == "frontend-build.spdx.json" else name.removesuffix(".spdx.json")}
            for name in inventories],
        "archives": [{"name": name, "sha256": digest(directory / name)} for name in names],
    }
    (directory / "release-inventory.json").write_text(json.dumps(index, indent=2) + "\n")
    shutil.copyfile(directory / "homebrew/Casks/gridctl.rb", directory / "gridctl.rb")
    names += inventories + ["release-inventory.json", "gridctl.rb"]
    checksums = "".join(f"{digest(directory / name)}  {name}\n" for name in names)
    (directory / "checksums.txt").write_text(checksums)
    # Bundles are produced last and are not checksummed into their own subjects.
    (directory / "attestation-subjects.txt").write_text(
        checksums + f"{digest(directory / 'checksums.txt')}  checksums.txt\n")
    return names + ["checksums.txt"]


def expected_assets(tag):
    names = archive_names(tag)
    return names + [name + ".spdx.json" for name in names] + [
        "frontend-build.spdx.json", "release-inventory.json", "gridctl.rb",
        "checksums.txt", "provenance.sigstore.json"]


def check_rejection(result, markers):
    diagnostic = (result.stdout + result.stderr).lower()
    if result.returncode != 1 or not any(marker.lower() in diagnostic for marker in markers):
        raise ValueError(f"expected an identity/cryptographic rejection; exit={result.returncode}; "
                         f"{result.stdout}{result.stderr}")


def verify(directory, tag, sha, repository, workflow, negative=False):
    check_source(f"refs/tags/{tag}", sha, sha)
    with tempfile.TemporaryDirectory(prefix="gridctl-verifier-") as clean:
        env = {key: value for key, value in os.environ.items()
               if key not in ("GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN")}
        env.update(HOME=clean, GH_CONFIG_DIR=clean, XDG_CACHE_HOME=clean, GH_HOST="github.com",
                   GH_FORCE_TTY="120", NO_COLOR="1")
        identity = f"https://github.com/{repository}/{workflow}@refs/tags/{tag}"
        policy = ["--repo", repository, "--cert-identity", identity,
                  "--source-ref", f"refs/tags/{tag}", "--source-digest", sha,
                  "--cert-oidc-issuer", "https://token.actions.githubusercontent.com",
                  "--predicate-type", PREDICATE, "--deny-self-hosted-runners"]
        bundle = directory / "provenance.sigstore.json"
        for name in expected_assets(tag):
            if name == bundle.name:
                continue
            subprocess.run(["gh", "attestation", "verify", str(directory / name),
                            "--bundle", str(bundle), *policy], env=env, check=True, timeout=180)
        if negative:
            archive = directory / archive_names(tag)[0]
            for flag, wrong, markers in (
                ("--repo", "wrong/repository", ("expected SourceRepositoryOwnerURI", "expected SourceRepositoryURI")),
                ("--cert-identity", identity.replace(workflow, ".github/workflows/wrong.yaml"),
                 ("no matching certificate identity", "expected SAN", "Sigstore verification failed")),
                ("--source-ref", "refs/tags/v0.0.0-wrong", ("expected SourceRepositoryRef",)),
                ("--source-digest", "0" * 40, ("expected SourceRepositoryDigest",)),
                ("--cert-oidc-issuer", "https://wrong.example", ("expected Issuer",)),
                ("--predicate-type", "https://wrong.example/predicate", ("no attestations found with predicate type",)),
            ):
                bad = list(policy)
                bad[bad.index(flag) + 1] = wrong
                result = subprocess.run(["gh", "attestation", "verify", str(archive),
                                         "--bundle", str(bundle), *bad], env=env,
                                        capture_output=True, text=True, timeout=180)
                check_rejection(result, markers)
                print(f"Rejected mismatch {flag}: {result.stderr.strip()}")
            tampered = Path(clean) / archive.name
            tampered.write_bytes(archive.read_bytes() + b"tampered")
            result = subprocess.run(["gh", "attestation", "verify", str(tampered),
                                     "--bundle", str(bundle), *policy], env=env,
                                    capture_output=True, text=True, timeout=180)
            check_rejection(result, ("artifact verification failed", "no matching subject",
                                     "unable to verify artifact", "Sigstore verification failed"))
            print(f"Rejected tampered archive: {result.stderr.strip()}")


def api(repository, path, method="GET", body=None, token=None):
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
        raise ValueError("invalid repository")
    headers = {"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28"}
    token = token if token is not None else os.environ["GITHUB_TOKEN"]
    if not token:
        raise ValueError("required repository credential is missing")
    headers["Authorization"] = f"Bearer {token}"
    data = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(f"https://api.github.com/repos/{repository}/{path}",
                                     data=data, headers=headers, method=method)
    with urllib.request.urlopen(request, timeout=120) as response:
        data = response.read()
    return json.loads(data) if data else None


def source(repository, ref, sha):
    check_source(ref, sha, sha)
    actual = api(repository, f"commits/{urllib.parse.quote(ref, safe='')}")["sha"]
    check_source(ref, sha, actual)


def release_record(repository, tag):
    try:
        return api(repository, f"releases/tags/{urllib.parse.quote(tag, safe='')}")
    except urllib.error.HTTPError as error:
        if error.code != 404:
            raise
        # The tag endpoint returns published releases. Drafts require a listing.
        for page in range(1, 101):
            records = api(repository, f"releases?per_page=100&page={page}")
            matches = [record for record in records if record.get("tag_name") == tag]
            if len(matches) > 1:
                raise ValueError("ambiguous draft releases for tag") from error
            if matches:
                return matches[0]
            if len(records) < 100:
                raise error
        raise ValueError("release lookup exceeded pagination limit") from error


def preflight(repository, ref, sha, mode):
    import yaml

    config = yaml.safe_load(Path(".goreleaser.yaml").read_text())
    destination = config["release"]["github"]
    if destination["owner"] + "/" + destination["name"] != repository:
        raise ValueError("GoReleaser destination does not match the source repository")
    source(repository, ref, sha)
    settings = None
    if mode == "immutable":
        try:
            settings = api(repository, "immutable-releases")
        except urllib.error.HTTPError as error:
            raise ValueError(f"immutable release settings unavailable (HTTP {error.code}); "
                             "a maintainer must enable immutable releases and grant Administration read") from error
    check_immutable(mode, settings)
    try:
        record = release_record(repository, ref.removeprefix("refs/tags/"))
    except urllib.error.HTTPError as error:
        if error.code == 404:
            return
        raise
    if record.get("draft") is not True:
        raise ValueError("release is already public; never replace assets under a published tag")


def download_assets(directory, repository, tag, public=False):
    record = release_record(repository, tag)
    if record.get("draft") is not (not public):
        raise ValueError("unexpected draft/public release state")
    assets = record["assets"]
    if sorted(asset["name"] for asset in assets) != sorted(expected_assets(tag)):
        raise ValueError("release asset inventory is incomplete or contains unexpected assets")
    directory.mkdir(parents=True, exist_ok=True)
    for asset in assets:
        if public:
            url = f"https://github.com/{repository}/releases/download/{tag}/{asset['name']}"
            headers = {}
        else:
            url = f"https://api.github.com/repos/{repository}/releases/assets/{asset['id']}"
            headers = {"Accept": "application/octet-stream",
                       "Authorization": f"Bearer {os.environ['GITHUB_TOKEN']}"}
        request = urllib.request.Request(url, headers=headers)
        with urllib.request.build_opener(DownloadRedirect()).open(request, timeout=180) as response:
            (directory / asset["name"]).write_bytes(response.read())
    return record


def upload(directory, repository, tag):
    record = release_record(repository, tag)
    if record.get("draft") is not True:
        raise ValueError("refuse to upload to a public release")
    existing = {asset["name"]: asset for asset in record["assets"]}
    for name in expected_assets(tag):
        path = directory / name
        if not path.is_file() or path.is_symlink() or path.stat().st_size == 0:
            raise ValueError(f"missing release asset: {name}")
        if name in existing:
            if existing[name].get("digest") == "sha256:" + digest(path):
                continue
            if name != "checksums.txt":
                raise ValueError(f"draft contains different bytes for {name}; recover the draft before retrying")
            api(repository, f"releases/assets/{existing[name]['id']}", "DELETE")
        url = (f"https://uploads.github.com/repos/{repository}/releases/{record['id']}/assets"
               f"?name={urllib.parse.quote(name, safe='')}")
        request = urllib.request.Request(url, data=path.read_bytes(), method="POST", headers={
            "Authorization": f"Bearer {os.environ['GITHUB_TOKEN']}",
            "Content-Type": "application/octet-stream"})
        with urllib.request.urlopen(request, timeout=180) as response:
            uploaded = json.load(response)
        if uploaded.get("digest") != "sha256:" + digest(path):
            raise ValueError(f"uploaded digest mismatch: {name}")


def publish(directory, repository, ref, sha, mode):
    preflight(repository, ref, sha, mode)
    tag = ref.removeprefix("refs/tags/")
    record = download_assets(directory, repository, tag)
    verify(directory, tag, sha, repository, ".github/workflows/release.yaml")
    # Resolve the remote tag again immediately before the irreversible transition.
    source(repository, ref, sha)
    published = api(repository, f"releases/{record['id']}", "PATCH", {"draft": False})
    if published.get("draft") is not False:
        raise ValueError("release did not become public")
    if mode == "immutable" and published.get("immutable") is not True:
        raise ValueError("published release is not immutable; stop and investigate, never replace its assets")
    print(f"Published {repository} {tag}; Homebrew has not yet advanced")
    download_assets(directory, repository, tag, public=True)
    verify(directory, tag, sha, repository, ".github/workflows/release.yaml")


def tap(directory, repository, tag, sha):
    import yaml

    config = yaml.safe_load(Path(".goreleaser.yaml").read_text())
    target = config["homebrew_casks"][0]["repository"]
    tap_repository = target["owner"] + "/" + target["name"]
    download_assets(directory, repository, tag, public=True)
    verify(directory, tag, sha, repository, ".github/workflows/release.yaml")
    token = os.environ["GORELEASER_TOKEN"]
    path = "contents/Casks/gridctl.rb"
    body = {"message": f"chore: update gridctl to {tag}",
            "content": base64.b64encode((directory / "gridctl.rb").read_bytes()).decode()}
    try:
        previous = api(tap_repository, path, token=token)
        body["sha"] = previous["sha"]
    except urllib.error.HTTPError as error:
        if error.code != 404:
            raise
    result = api(tap_repository, path, "PUT", body, token=token)
    expected = hashlib.sha1(b"blob " + str((directory / "gridctl.rb").stat().st_size).encode()
                            + b"\0" + (directory / "gridctl.rb").read_bytes()).hexdigest()
    if result["content"]["sha"] != expected:
        raise ValueError("tap content differs from authenticated generated cask")
    print(f"Advanced {tap_repository} after verifying all public assets for {tag}")


def main():
    if sys.argv[1:] == ["gates"]:
        check_gates(json.loads(os.environ["RELEASE_GATES"]))
        return
    command = sys.argv[1]
    repository = os.environ["GITHUB_REPOSITORY"]
    ref = os.environ.get("RELEASE_REF", os.environ["GITHUB_REF"])
    sha = os.environ["GITHUB_SHA"]
    tag = ref.removeprefix("refs/tags/")
    directory = Path(os.environ.get("RELEASE_DIST", "dist"))
    if command == "preflight":
        preflight(repository, ref, sha, os.environ["RELEASE_MODE"])
    elif command == "prepare":
        prepare(directory, tag, sha, os.environ["SPDX_SCHEMA"])
    elif command == "verify":
        verify(directory, tag, sha, repository, ".github/workflows/release.yaml", negative=True)
    elif command == "download":
        download_assets(directory, repository, tag, public=os.environ.get("RELEASE_PUBLIC") == "true")
    elif command == "upload":
        upload(directory, repository, tag)
    elif command == "publish":
        publish(directory, repository, ref, sha, os.environ["RELEASE_MODE"])
    elif command == "tap":
        tap(directory, repository, tag, sha)
    else:
        raise ValueError("unknown release command")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, OSError, subprocess.SubprocessError) as error:
        sys.exit(f"release prerequisite failed: {error}")
