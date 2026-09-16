"""Policy for the MCP Python runtime base recipe, tags, and evidence."""

from __future__ import annotations

import base64
import hashlib
import io
import json
from pathlib import Path
import platform
import re
import sys
import tarfile
import urllib.request


ROOT = Path(__file__).resolve().parents[1]
RECIPE = ROOT / "images" / "mcp-runtime-python"
PINS = RECIPE / "pins.yaml"
WORKFLOW = ROOT / ".github" / "workflows" / "mcp-runtime-python.yaml"
GENERATOR = ROOT / "pkg" / "builder" / "python_template.go"
PYPROJECT = RECIPE / "fixtures" / "echo-server" / "pyproject.toml"
CONSTRAINTS = RECIPE / "fixtures" / "echo-server" / "constraints.txt"

TOOLS = {
    "syft": ("anchore/syft", "v1.42.0", {
        "Linux-x86_64": ("syft_1.42.0_linux_amd64.tar.gz", "syft",
                         "23bec7de5db0ba05590c676a338a8cd49e635df63e6c404c34d437e2c57f1a77"),
        "Linux-aarch64": ("syft_1.42.0_linux_arm64.tar.gz", "syft",
                          "cbc39a5f29b0bd32c1bf6bf61c363373f20a1be39dd901e1869228d42d082121"),
    }),
    "grype": ("anchore/grype", "v0.104.0", {
        "Linux-x86_64": ("grype_0.104.0_linux_amd64.tar.gz", "grype",
                         "862816d0addab60968f9401fe5dbaeaf244bf86a3f759003103c11efe151b31d"),
        "Linux-aarch64": ("grype_0.104.0_linux_arm64.tar.gz", "grype",
                          "531c9fcc6dab7efd5eccee629bc4c524badf3d66c5382fc0e771059b76dc617e"),
    }),
}

LOCKED_DOCKERFILE = RECIPE / "fixtures" / "echo-server" / "Dockerfile.locked"
HASHED_DOCKERFILE = RECIPE / "fixtures" / "echo-server" / "Dockerfile.hashed"
BASE_DOCKERFILE = RECIPE / "Dockerfile"

REQUIRED_PLATFORMS = ("linux/amd64", "linux/arm64")
WORKFLOW_PATH = ".github/workflows/mcp-runtime-python.yaml"
SPDX_PREDICATE = "https://spdx.dev/Document"
SLSA_PREDICATE = "https://slsa.dev/provenance/v1"
EVIDENCE_PREDICATE = "https://gridctl.dev/mcp-runtime-python-evidence/v1"
ABSENT_MARKERS = (
    "manifest unknown",
    "no such manifest",
    "name unknown",
    "repository does not exist",
    "manifest unknown to registry",
)
LOOKUP_FAILURE_MARKERS = (
    "unauthorized",
    "authentication",
    "denied",
    "forbidden",
    "toomanyrequests",
    "too many requests",
    "timeout",
    "timed out",
    "connection refused",
    "connection reset",
    "tls",
    "network is unreachable",
    "temporary failure",
    "server error",
    "internal server",
    " 429 ",
    " 500 ",
    " 502 ",
    " 503 ",
)


def load_yaml(path):
    import yaml
    return yaml.safe_load(path.read_text())


def load_pins():
    pins = load_yaml(PINS)
    if not isinstance(pins, dict) or pins.get("package") != "ghcr.io/gridctl/mcp-runtime-python":
        raise ValueError("pins must declare the approved package name")
    return pins


def _require(condition, message):
    if not condition:
        raise ValueError(message)


def validate_recipe():
    pins = load_pins()
    base = BASE_DOCKERFILE.read_text()
    locked = LOCKED_DOCKERFILE.read_text()
    hashed = HASHED_DOCKERFILE.read_text()
    generator = GENERATOR.read_text()
    python_image = pins["python"]["image"]
    uv_image = pins["uv"]["image"]
    uid = pins["identity"]["uid"]
    gid = pins["identity"]["gid"]
    tini_version = pins["init"]["version"]
    snapshot = pins["init"]["debian_snapshot"]
    setuptools = pins["build_system"]["setuptools"]
    pyproject = PYPROJECT.read_text()
    constraints = CONSTRAINTS.read_text()

    _require(python_image in base, "base Dockerfile must pin the reviewed Python image")
    _require(f"USER {uid}:{gid}" in base, "base image USER must be the numeric identity")
    _require("ENTRYPOINT [\"/usr/bin/tini\", \"--\"]" in base, "base image must use tini as ENTRYPOINT")
    _require(not any(line.startswith("VOLUME") for line in base.splitlines()), "base image must not declare VOLUME")
    _require("PYTHONDONTWRITEBYTECODE=1" in base, "base image must disable bytecode on the read-only root")
    _require("HOME=/tmp" in base, "HOME must be the default scratch mount")
    _require("not an MCP server" in base, "base CMD must refuse to pose as a server")
    _require(f"TINI_VERSION={tini_version}" in base, "tini must be installed at the reviewed package version")
    _require("tini=${TINI_VERSION}" in base, "tini install must use the reviewed version argument")
    _require(f"DEBIAN_SNAPSHOT={snapshot}" in base, "tini must install from the reviewed Debian snapshot")
    _require("${DEBIAN_SNAPSHOT}" in base, "apt sources must use the reviewed Debian snapshot")
    _require("chown root:root /app /data" in base, "application and data paths must stay root-owned")
    _require("uv" not in base.lower(), "base image must not retain uv")

    for name, text in (("locked", locked), ("hashed", hashed)):
        _require(f"ARG RUNTIME_BASE=" in text, f"{name} derivative must take the runtime base as a build arg")
        _require(uv_image in text, f"{name} derivative must pin the reviewed uv image")
        _require(python_image in text, f"{name} derivative build stage must pin the reviewed Python image")
        _require("FROM ${RUNTIME_BASE}" in text, f"{name} derivative must copy onto the runtime base")
        _require("CMD [\"mcp-echo-fixture\"]" in text, f"{name} derivative must execute the console script")
        _require("chown -R root:root /app" in text, f"{name} derivative application files must stay root-owned")
        _require("USER 0" in text, f"{name} derivative must install as root")
        _require(f"USER {uid}:{gid}" in text, f"{name} derivative must restore the numeric identity")
        _require("constraints.txt" in text, f"{name} derivative must apply the reviewed build-system constraints")
        _require(f"setuptools=={setuptools}" in text or f"setuptools=={setuptools}" in constraints,
                 f"{name} derivative must freeze setuptools")

    hashed_installer = (RECIPE / "fixtures" / "echo-server" / "hash_install.py").read_text()
    _require("--locked" in locked, "locked derivative must install from the lockfile")
    _require("--require-hashes" in hashed_installer, "hashed derivative must install with hashes")
    _require("hash_install.py" in hashed, "hashed derivative must run the hashed installer")
    _require(f'setuptools=={setuptools}' in pyproject, "build-system setuptools must be an exact reviewed pin")
    _require(f"setuptools=={setuptools}" in constraints, "constraints.txt must freeze setuptools")
    _require("mcp-runtime-python" not in generator, "generated Python Dockerfiles must not switch to this base")
    _require("python:3.10" in generator and "python:3.13" in generator, "generated Python 3.10-3.13 support must remain")
    _require("chown -R gridctl:gridctl" in generator, "generated images still chown application directories")
    _require("COPY --from=" in generator and "/uv /uvx /bin/" in generator, "generated images still retain uv")
    return pins


def validate_workflow():
    workflow = load_yaml(WORKFLOW)
    _require(workflow.get("permissions") == {}, "workflow default permissions must be empty")
    jobs = workflow["jobs"]
    _require(jobs["validate"]["permissions"] == {"contents": "read"}, "validate job must be read-only")
    for name in ("test-amd64", "test-arm64"):
        _require(jobs[name]["permissions"] == {"contents": "read"}, f"{name} must be read-only")
        _require("packages" not in jobs[name]["permissions"], f"{name} must not push")
    publish = jobs["publish-candidate"]
    _require(publish["permissions"]["packages"] == "write", "candidate publish needs packages write")
    _require(publish["permissions"]["id-token"] == "write", "candidate publish needs id-token write")
    _require(publish["permissions"]["attestations"] == "write", "candidate publish needs attestations write")
    _require(publish["permissions"]["contents"] == "read", "candidate publish must not request contents write")
    triggers = workflow.get("on", workflow.get(True))
    _require(isinstance(triggers, dict) and "workflow_dispatch" in triggers, "publication remains an explicit dispatch")
    _require("promote_supported" in triggers["workflow_dispatch"]["inputs"], "supported aliases stay a separate input")
    _require(jobs["promote-supported"]["if"] and "promote_supported" in str(jobs["promote-supported"]["if"]),
             "supported promotion must stay gated on its input")
    _require(jobs["publish-candidate"]["if"] and "publish_candidate" in str(jobs["publish-candidate"]["if"]),
             "candidate publication must stay gated on its input")
    _require("verify_anonymous" in str(jobs["anonymous-verify"].get("if")),
             "anonymous verification must stay gated on its input")
    _require("skipped" in str(jobs["anonymous-verify"].get("if")),
             "anonymous verification must run without republishing when publish-candidate is skipped")
    promote_needs = jobs["promote-supported"].get("needs") or []
    if isinstance(promote_needs, str):
        promote_needs = [promote_needs]
    _require("test-amd64" not in promote_needs and "test-arm64" not in promote_needs,
             "promotion must not treat dispatch-checkout tests as candidate evidence")
    _require("publish-candidate" not in promote_needs,
             "promotion is separately authorized for an existing candidate")
    text = WORKFLOW.read_text()
    _require("fail-on critical" in text, "vulnerability gate must fail on critical findings")
    _require("ubuntu-24.04-arm" in text and "ubuntu-24.04" in text, "native amd64 and arm64 runners are required")
    _require("docker.io" not in text.lower() or "GRIDCTL_RUNTIME" in text, "workflow should not assume a second runtime silently")
    _require("require-absent" in text, "immutable tags must fail closed on lookup errors")
    _require("require-native" in text, "hosted jobs must refuse emulated architecture evidence")
    _require("require-executed" in text, "hosted jobs must refuse skipped acceptance tests")
    _require("GRIDCTL_REQUIRE_MCP_RUNTIME" in text, "hosted jobs must require a container runtime")
    _require(SPDX_PREDICATE in text, "SBOMs must be attested with the SPDX predicate")
    _require(EVIDENCE_PREDICATE in text, "publication must attest the digest-bound evidence inventory")
    _require("verify-candidate" in text, "anonymous verification and promotion must share the candidate evidence check")
    publish_start = text.find("publish-candidate:")
    grype_at = text.find("grype", publish_start)
    push_at = text.find("docker push", publish_start)
    _require(publish_start != -1 and 0 <= grype_at < push_at,
             "vulnerability gate must run before pushing candidate tags")
    return workflow


def revision_tag(sha):
    if not re.fullmatch(r"[0-9a-f]{40}", sha):
        raise ValueError("revision tags require a full source SHA")
    return f"sha-{sha}"


def candidate_tag(sha):
    return f"candidate-{revision_tag(sha)}"


def platform_tag(sha, architecture):
    if architecture not in {"amd64", "arm64"}:
        raise ValueError("platform tags are amd64 or arm64")
    return f"{revision_tag(sha)}-{architecture}"


def assert_immutable_tag(tag, pins=None):
    pins = pins or load_pins()
    if (re.fullmatch(pins["tags"]["revision"], tag)
            or re.fullmatch(pins["tags"]["candidate"], tag)
            or re.fullmatch(pins["tags"]["revision_platform"], tag)):
        return tag
    raise ValueError(f"not an immutable revision tag: {tag}")


def refuse_overwrite(existing, tag):
    assert_immutable_tag(tag)
    if tag in existing:
        raise ValueError(f"refusing to overwrite immutable tag {tag}")
    return tag


def classify_manifest_lookup(returncode, output):
    text = f" {output or ''} ".lower()
    if int(returncode) == 0:
        return "present"
    failed = any(marker in text for marker in LOOKUP_FAILURE_MARKERS)
    absent = any(marker in text for marker in ABSENT_MARKERS)
    if failed or not absent:
        raise ValueError("manifest lookup failed; refuse to treat the tag as absent")
    return "absent"


def require_absent(returncode, output, tag):
    assert_immutable_tag(tag)
    status = classify_manifest_lookup(returncode, output)
    if status == "present":
        raise ValueError(f"refusing to overwrite immutable tag {tag}")
    return tag


def classify_subject(kind, digest):
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
        raise ValueError("OCI subjects require a sha256 digest")
    if kind not in {"index", "platform"}:
        raise ValueError("subject kind must be index or platform")
    return {"kind": kind, "digest": digest}


def _sbom_record(name, spec, subject_digest):
    if not isinstance(spec, dict):
        raise ValueError("SBOMs must bind a file digest to a subject digest")
    digest = spec.get("sha256", "")
    if not re.fullmatch(r"[0-9a-f]{64}", digest):
        raise ValueError("SBOM sha256 required")
    if spec.get("subject") != subject_digest:
        raise ValueError(f"SBOM {name} subject does not match the {name} manifest digest")
    path = spec.get("path")
    if not path or not str(path).endswith(".spdx.json"):
        raise ValueError("SBOM path must be an SPDX JSON file")
    return {"path": path, "sha256": digest, "subject": subject_digest, "scope": name}


def evidence_inventory(index_digest, platforms, sboms):
    index = classify_subject("index", index_digest)
    platform_subjects = []
    for platform_name, digest in platforms.items():
        if not re.fullmatch(r"linux/(amd64|arm64)", platform_name):
            raise ValueError(f"unsupported platform subject {platform_name}")
        platform_subjects.append({"platform": platform_name, **classify_subject("platform", digest)})
    expected = {item["platform"] for item in platform_subjects} | {"index"}
    if set(sboms) != expected:
        raise ValueError("SBOMs must cover the index and each platform manifest")
    subjects = {"index": index_digest, **platforms}
    normalized = {name: _sbom_record(name, spec, subjects[name]) for name, spec in sboms.items()}
    return {"index": index, "platforms": platform_subjects, "sboms": normalized}


def file_sha256(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def write_attestation_subjects(directory, names):
    directory = Path(directory)
    lines = []
    for name in names:
        path = directory / name
        if path.is_symlink() or not path.is_file() or path.stat().st_size == 0:
            raise ValueError(f"missing, empty, or symlinked evidence file: {name}")
        lines.append(f"{file_sha256(path)}  {name}\n")
    subjects = directory / "attestation-subjects.txt"
    subjects.write_text("".join(lines))
    return subjects


def parse_index_subjects(payload):
    data = json.loads(payload) if isinstance(payload, str) else payload
    if not isinstance(data, dict):
        raise ValueError("index inspect payload must be an object")
    digest = data.get("digest") or data.get("Digest")
    manifests = data.get("manifests") or data.get("Manifests")
    for key in ("manifest", "Manifest"):
        inner = data.get(key)
        if isinstance(inner, dict):
            digest = digest or inner.get("digest") or inner.get("Digest")
            manifests = manifests or inner.get("manifests") or inner.get("Manifests")
        elif isinstance(inner, list) and not manifests:
            manifests = inner
    if not digest:
        descriptor = data.get("descriptor") or data.get("Descriptor") or {}
        digest = descriptor.get("digest") or descriptor.get("Digest")
    if not digest:
        raise ValueError("index inspect payload missing digest")
    if not manifests:
        manifests = []
    platforms = {}
    for item in manifests:
        platform = item.get("platform") or {}
        os_name = platform.get("os")
        architecture = platform.get("architecture")
        if os_name != "linux" or architecture not in {"amd64", "arm64"}:
            continue
        if platform.get("variant"):
            continue
        platforms[f"linux/{architecture}"] = item.get("digest")
    inventory = evidence_inventory(
        digest,
        platforms,
        {name: {"path": f"{name.replace('/', '-')}.spdx.json", "sha256": "0" * 64, "subject": subject}
         for name, subject in {"index": digest, **platforms}.items()},
    )
    return {"index": inventory["index"]["digest"], "platforms": platforms}


def bind_evidence(index_digest, platforms, sbom_files, source_sha, scan_status="passed"):
    if scan_status != "passed":
        raise ValueError("refusing to bind evidence for a scan-failed candidate")
    sboms = {}
    subjects = {"index": index_digest, **platforms}
    for name, path in sbom_files.items():
        sboms[name] = {
            "path": Path(path).name,
            "sha256": file_sha256(path),
            "subject": subjects[name],
        }
    inventory = evidence_inventory(index_digest, platforms, sboms)
    inventory["source_sha"] = check_source(source_sha, source_sha)
    inventory["scan_status"] = scan_status
    inventory["workflow"] = WORKFLOW_PATH
    inventory["repository"] = "gridctl/gridctl"
    inventory["package"] = "ghcr.io/gridctl/mcp-runtime-python"
    return inventory


def native_execution(runner_arch, image_platform):
    mapping = {"x86_64": "linux/amd64", "amd64": "linux/amd64", "aarch64": "linux/arm64", "arm64": "linux/arm64"}
    expected = mapping.get(runner_arch)
    if expected is None:
        return {"image_platform": image_platform, "runner": runner_arch, "mode": "unknown"}
    if expected == image_platform:
        return {"image_platform": image_platform, "runner": runner_arch, "mode": "native"}
    return {"image_platform": image_platform, "runner": runner_arch, "mode": "emulated"}


def require_native(runner_arch, image_platform):
    result = native_execution(runner_arch, image_platform)
    if result["mode"] != "native":
        raise ValueError(f"hosted acceptance requires native execution, got {result['mode']}")
    return result


def check_source(expected, actual):
    if not re.fullmatch(r"[0-9a-f]{40}", expected) or actual != expected:
        raise ValueError("image publication source mismatch; refuse publication")
    return actual


def require_executed(events_text, test_name):
    seen = False
    skipped = False
    passed = False
    failed = False
    for line in events_text.splitlines():
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            event = json.loads(line)
        except json.JSONDecodeError as error:
            raise ValueError("go test JSON capture is not valid") from error
        if event.get("Test") != test_name:
            continue
        action = event.get("Action")
        if action == "run":
            seen = True
        elif action == "skip":
            skipped = True
        elif action == "pass":
            passed = True
        elif action == "fail":
            failed = True
    if skipped:
        raise ValueError(f"required acceptance test skipped: {test_name}")
    if failed or not seen or not passed:
        raise ValueError(f"required acceptance test did not pass: {test_name}")
    return test_name


def assert_promotable(request):
    candidate_sha = request.get("candidate_sha") or ""
    attested = request.get("attested_source_sha") or ""
    check_source(candidate_sha, attested)
    if request.get("scan_status") != "passed":
        raise ValueError("refusing to promote a scan-failed candidate")
    if not request.get("public_evidence"):
        raise ValueError("missing public evidence")
    if WORKFLOW_PATH not in str(request.get("workflow") or ""):
        raise ValueError("promotion must authenticate the image workflow identity")
    platforms = request.get("platforms") or {}
    if set(platforms) != set(REQUIRED_PLATFORMS):
        raise ValueError("promotion requires both architecture digests")
    index = request["index_digest"]
    classify_subject("index", index)
    for digest in platforms.values():
        classify_subject("platform", digest)
    required = {index, *platforms.values()}
    if set(request.get("sbom_subjects") or []) != required:
        raise ValueError("promotion SBOMs missing or incorrectly scoped")
    if request.get("candidate_digest") != index:
        raise ValueError("promotion must retag the attested candidate index digest")
    return {"index": index, "platforms": platforms}


def _decode_dsse_payload(item):
    raw = ((item.get("bundle") or {}).get("dsseEnvelope") or {}).get("payload")
    if not raw:
        return None
    return json.loads(base64.b64decode(raw))


def summarize_github_attestations(api_json, source_sha=""):
    attested_subjects = []
    sbom_subjects = []
    predicate_types = []
    saw_workflow = False
    saw_source = False
    scan_status = ""
    for item in api_json.get("attestations") or []:
        payload = _decode_dsse_payload(item)
        if not payload:
            continue
        encoded = json.dumps(payload)
        if WORKFLOW_PATH in encoded:
            saw_workflow = True
        if source_sha and source_sha in encoded:
            saw_source = True
        ptype = payload.get("predicateType")
        if ptype:
            predicate_types.append(ptype)
        subjects = []
        for subject in payload.get("subject") or []:
            digest = (subject.get("digest") or {}).get("sha256")
            if digest:
                subjects.append("sha256:" + digest)
        attested_subjects.extend(subjects)
        if ptype == SPDX_PREDICATE:
            sbom_subjects.extend(subjects)
        if ptype == EVIDENCE_PREDICATE:
            scan_status = (payload.get("predicate") or {}).get("scan_status") or scan_status
    return {
        "attested_subjects": sorted(set(attested_subjects)),
        "sbom_subjects": sorted(set(sbom_subjects)),
        "predicate_types": sorted(set(predicate_types)),
        "workflow": WORKFLOW_PATH if saw_workflow else "",
        "source_sha": source_sha if saw_source else "",
        "scan_status": scan_status,
        "repository": "gridctl/gridctl",
    }


def verify_public_evidence(document, expected):
    if not document.get("anonymous"):
        raise ValueError("public verification must be anonymous")
    if document.get("image_digest") != expected["index_digest"]:
        raise ValueError("pulled image digest does not match the candidate index")
    if document.get("repository") != expected["repository"]:
        raise ValueError("attestation repository mismatch")
    if WORKFLOW_PATH not in str(document.get("workflow") or ""):
        raise ValueError("attestation workflow mismatch")
    if document.get("source_sha") != expected["source_sha"]:
        raise ValueError("attestation source mismatch")
    subjects = set(document.get("attested_subjects") or [])
    required = {expected["index_digest"], *expected["platforms"].values()}
    if not required.issubset(subjects):
        raise ValueError("public evidence missing index or platform subjects")
    if set(document.get("sbom_subjects") or []) != required:
        raise ValueError("public SBOMs missing or incorrectly scoped")
    if document.get("scan_status") != "passed":
        raise ValueError("public evidence missing a passed vulnerability scan")
    predicates = set(document.get("predicate_types") or [])
    if SPDX_PREDICATE not in predicates or SLSA_PREDICATE not in predicates or EVIDENCE_PREDICATE not in predicates:
        raise ValueError("public evidence must include provenance, SPDX SBOMs, and the evidence inventory")
    return expected["index_digest"]


def verify_candidate(subjects, api_json, candidate_sha):
    summary = summarize_github_attestations(api_json, candidate_sha)
    document = {
        "anonymous": True,
        "image_digest": subjects["index"],
        "repository": summary["repository"],
        "workflow": summary["workflow"],
        "source_sha": summary["source_sha"],
        "attested_subjects": summary["attested_subjects"],
        "sbom_subjects": summary["sbom_subjects"],
        "predicate_types": summary["predicate_types"],
        "scan_status": summary["scan_status"],
    }
    expected = {
        "index_digest": subjects["index"],
        "platforms": subjects["platforms"],
        "repository": "gridctl/gridctl",
        "source_sha": candidate_sha,
    }
    verify_public_evidence(document, expected)
    return assert_promotable({
        "candidate_sha": candidate_sha,
        "attested_source_sha": summary["source_sha"],
        "candidate_digest": subjects["index"],
        "index_digest": subjects["index"],
        "platforms": subjects["platforms"],
        "sbom_subjects": summary["sbom_subjects"],
        "scan_status": summary["scan_status"],
        "public_evidence": True,
        "workflow": summary["workflow"],
    })


def fetch_github_attestations(digests):
    attestations = []
    for digest in digests:
        if not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
            raise ValueError("attestation fetch requires sha256 digests")
        url = "https://api.github.com/repos/gridctl/gridctl/attestations/" + digest
        request = urllib.request.Request(url, headers={
            "User-Agent": "gridctl-mcp-runtime-python",
            "Accept": "application/vnd.github+json",
        })
        try:
            with urllib.request.urlopen(request, timeout=60) as response:
                payload = json.loads(response.read())
        except Exception as error:
            raise ValueError(f"anonymous attestation fetch failed for {digest}") from error
        attestations.extend(payload.get("attestations") or [])
    if not attestations:
        raise ValueError("anonymous attestation fetch returned no public evidence")
    return {"attestations": attestations}


def download(url, digest):
    with urllib.request.urlopen(url, timeout=120) as response:
        data = response.read()
    if hashlib.sha256(data).hexdigest() != digest:
        raise ValueError(f"bootstrap checksum mismatch: {url}")
    return data


def install_tools(destination, *names):
    destination = Path(destination)
    destination.mkdir(parents=True, exist_ok=True)
    host = f"{platform.system()}-{platform.machine()}"
    selected = names or ("syft", "grype")
    for name in selected:
        repository, version, targets = TOOLS[name]
        archive, member, digest = targets[host]
        data = download(f"https://github.com/{repository}/releases/download/{version}/{archive}", digest)
        with tarfile.open(fileobj=io.BytesIO(data), mode="r:gz") as source:
            with source.extractfile(member) as executable:
                binary = executable.read()
        target = destination / name
        target.write_bytes(binary)
        target.chmod(0o755)
        print(f"Installed {name} {version} ({host}), pinned SHA256 verified")


def _load_json_arg(value):
    path = Path(value)
    if path.is_file():
        return json.loads(path.read_text())
    return json.loads(value)


def main(argv=None):
    argv = list(sys.argv[1:] if argv is None else argv)
    if not argv:
        raise ValueError("command required")
    command = argv[0]
    if command == "validate":
        validate_recipe()
        validate_workflow()
        print("mcp-runtime-python recipe and workflow policy accepted")
        return
    if command == "install-tools":
        install_tools(argv[1], *argv[2:])
        return
    if command == "check-source":
        print(check_source(argv[1], argv[2]))
        return
    if command == "revision-tag":
        print(revision_tag(argv[1]))
        return
    if command == "candidate-tag":
        print(candidate_tag(argv[1]))
        return
    if command == "platform-tag":
        print(platform_tag(argv[1], argv[2]))
        return
    if command == "refuse-overwrite":
        existing = json.loads(argv[1])
        print(refuse_overwrite(existing, argv[2]))
        return
    if command == "require-absent":
        print(require_absent(argv[1], argv[2], argv[3]))
        return
    if command == "subjects":
        inventory = evidence_inventory(argv[1], json.loads(argv[2]), json.loads(argv[3]))
        json.dump(inventory, sys.stdout)
        print()
        return
    if command == "parse-index":
        json.dump(parse_index_subjects(argv[1] if argv[1].lstrip().startswith("{") else Path(argv[1]).read_text()),
                  sys.stdout)
        print()
        return
    if command == "bind-evidence":
        payload = _load_json_arg(argv[1])
        inventory = bind_evidence(
            payload["index"], payload["platforms"], payload["sboms"],
            payload["source_sha"], payload.get("scan_status", "passed"))
        destination = Path(argv[2]) if len(argv) > 2 else None
        encoded = json.dumps(inventory, indent=2) + "\n"
        if destination:
            destination.write_text(encoded)
        else:
            sys.stdout.write(encoded)
        return
    if command == "assert-promotable":
        json.dump(assert_promotable(_load_json_arg(argv[1])), sys.stdout)
        print()
        return
    if command == "verify-public-evidence":
        print(verify_public_evidence(_load_json_arg(argv[1]), _load_json_arg(argv[2])))
        return
    if command == "verify-candidate":
        json.dump(verify_candidate(_load_json_arg(argv[1]), _load_json_arg(argv[2]), argv[3]), sys.stdout)
        print()
        return
    if command == "fetch-attestations":
        subjects = _load_json_arg(argv[1])
        json.dump(fetch_github_attestations([subjects["index"], *subjects["platforms"].values()]), sys.stdout)
        print()
        return
    if command == "summarize-attestations":
        source_sha = argv[2] if len(argv) > 2 else ""
        json.dump(summarize_github_attestations(_load_json_arg(argv[1]), source_sha), sys.stdout)
        print()
        return
    if command == "write-subjects":
        write_attestation_subjects(argv[1], argv[2:])
        return
    if command == "native-mode":
        json.dump(native_execution(argv[1], argv[2]), sys.stdout)
        print()
        return
    if command == "require-native":
        json.dump(require_native(argv[1], argv[2]), sys.stdout)
        print()
        return
    if command == "require-executed":
        print(require_executed(Path(argv[1]).read_text(), argv[2]))
        return
    raise ValueError(f"unknown command {command}")


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(error, file=sys.stderr)
        raise SystemExit(1) from error
