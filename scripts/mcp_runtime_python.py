"""Policy for the MCP Python runtime base recipe, tags, and evidence."""

from __future__ import annotations

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

    _require(python_image in base, "base Dockerfile must pin the reviewed Python image")
    _require(f"USER {uid}:{gid}" in base, "base image USER must be the numeric identity")
    _require("ENTRYPOINT [\"/usr/bin/tini\", \"--\"]" in base, "base image must use tini as ENTRYPOINT")
    _require(not any(line.startswith("VOLUME") for line in base.splitlines()), "base image must not declare VOLUME")
    _require("PYTHONDONTWRITEBYTECODE=1" in base, "base image must disable bytecode on the read-only root")
    _require("HOME=/tmp" in base, "HOME must be the default scratch mount")
    _require("not an MCP server" in base, "base CMD must refuse to pose as a server")
    _require("apt-get install -y --no-install-recommends tini" in base, "tini must come from the distro of the pinned base")
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

    hashed_installer = (RECIPE / "fixtures" / "echo-server" / "hash_install.py").read_text()
    _require("--locked" in locked, "locked derivative must install from the lockfile")
    _require("--require-hashes" in hashed_installer, "hashed derivative must install with hashes")
    _require("hash_install.py" in hashed, "hashed derivative must run the hashed installer")
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
    text = WORKFLOW.read_text()
    _require("fail-on critical" in text, "vulnerability gate must fail on critical findings")
    _require("ubuntu-24.04-arm" in text and "ubuntu-24.04" in text, "native amd64 and arm64 runners are required")
    _require("docker.io" not in text.lower() or "GRIDCTL_RUNTIME" in text, "workflow should not assume a second runtime silently")
    return workflow


def revision_tag(sha):
    if not re.fullmatch(r"[0-9a-f]{40}", sha):
        raise ValueError("revision tags require a full source SHA")
    return f"sha-{sha}"


def candidate_tag(sha):
    return f"candidate-{revision_tag(sha)}"


def assert_immutable_tag(tag, pins=None):
    pins = pins or load_pins()
    if re.fullmatch(pins["tags"]["revision"], tag) or re.fullmatch(pins["tags"]["candidate"], tag):
        return tag
    raise ValueError(f"not an immutable revision tag: {tag}")


def refuse_overwrite(existing, tag):
    assert_immutable_tag(tag)
    if tag in existing:
        raise ValueError(f"refusing to overwrite immutable tag {tag}")
    return tag


def classify_subject(kind, digest):
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
        raise ValueError("OCI subjects require a sha256 digest")
    if kind not in {"index", "platform"}:
        raise ValueError("subject kind must be index or platform")
    return {"kind": kind, "digest": digest}


def evidence_inventory(index_digest, platforms, sboms):
    index = classify_subject("index", index_digest)
    platform_subjects = []
    for platform_name, digest in platforms.items():
        if not re.fullmatch(r"linux/(amd64|arm64)", platform_name):
            raise ValueError(f"unsupported platform subject {platform_name}")
        platform_subjects.append({"platform": platform_name, **classify_subject("platform", digest)})
    if set(sboms) != {item["platform"] for item in platform_subjects} | {"index"}:
        raise ValueError("SBOMs must cover the index and each platform manifest")
    return {"index": index, "platforms": platform_subjects, "sboms": sboms}


def native_execution(runner_arch, image_platform):
    mapping = {"x86_64": "linux/amd64", "amd64": "linux/amd64", "aarch64": "linux/arm64", "arm64": "linux/arm64"}
    expected = mapping.get(runner_arch)
    if expected is None:
        return {"image_platform": image_platform, "runner": runner_arch, "mode": "unknown"}
    if expected == image_platform:
        return {"image_platform": image_platform, "runner": runner_arch, "mode": "native"}
    return {"image_platform": image_platform, "runner": runner_arch, "mode": "emulated"}


def check_source(expected, actual):
    if not re.fullmatch(r"[0-9a-f]{40}", expected) or actual != expected:
        raise ValueError("image publication source mismatch; refuse publication")
    return actual


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
    if command == "refuse-overwrite":
        existing = json.loads(argv[1])
        print(refuse_overwrite(existing, argv[2]))
        return
    if command == "subjects":
        inventory = evidence_inventory(argv[1], json.loads(argv[2]), json.loads(argv[3]))
        json.dump(inventory, sys.stdout)
        print()
        return
    if command == "native-mode":
        json.dump(native_execution(argv[1], argv[2]), sys.stdout)
        print()
        return
    raise ValueError(f"unknown command {command}")


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(error, file=sys.stderr)
        raise SystemExit(1) from error
