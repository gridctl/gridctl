"""Install reviewed release tools into an explicit, caller-owned directory."""

import hashlib
import io
from pathlib import Path
import platform
import sys
import tarfile
import urllib.request
import zipfile


TOOLS = {
    "goreleaser": ("goreleaser/goreleaser", "v2.14.3", {
        "Linux-x86_64": ("goreleaser_Linux_x86_64.tar.gz", "goreleaser",
                          "dc7faeeeb6da8bdfda788626263a4ae725892a8c7504b975c3234127d4a44579"),
    }),
    "syft": ("anchore/syft", "v1.42.0", {
        "Linux-x86_64": ("syft_1.42.0_linux_amd64.tar.gz", "syft",
                          "23bec7de5db0ba05590c676a338a8cd49e635df63e6c404c34d437e2c57f1a77"),
    }),
    "gh": ("cli/cli", "v2.87.3", {
        "Linux-x86_64": ("gh_2.87.3_linux_amd64.tar.gz", "gh_2.87.3_linux_amd64/bin/gh",
                          "c6e5537631fca45f277ef405ce8751d139b491e9402cc20891a003525a8773b2"),
        "Darwin-x86_64": ("gh_2.87.3_macOS_amd64.zip", "gh_2.87.3_macOS_amd64/bin/gh",
                           "7b8d5495fe9689494b1c69559c0d28209bc057bb028008e903ec4b3e19bd8c75"),
        "Darwin-arm64": ("gh_2.87.3_macOS_arm64.zip", "gh_2.87.3_macOS_arm64/bin/gh",
                          "dedfc6f569e9dbc5b92d47dce44acadbdf5b6b7a861510db0c748dfac55002f6"),
    }),
}


def download(url, digest):
    with urllib.request.urlopen(url, timeout=120) as response:
        data = response.read()
    if hashlib.sha256(data).hexdigest() != digest:
        raise ValueError(f"bootstrap checksum mismatch: {url}")
    return data


def main():
    destination = Path(sys.argv[1])
    destination.mkdir(parents=True, exist_ok=True)
    host = f"{platform.system()}-{platform.machine()}"
    for name in sys.argv[2:]:
        repository, version, targets = TOOLS[name]
        archive, member, digest = targets[host]
        data = download(f"https://github.com/{repository}/releases/download/{version}/{archive}",
                        digest)
        if archive.endswith(".zip"):
            with zipfile.ZipFile(io.BytesIO(data)) as source:
                binary = source.read(member)
        else:
            with tarfile.open(fileobj=io.BytesIO(data), mode="r:gz") as source:
                with source.extractfile(member) as executable:
                    binary = executable.read()
        target = destination / name
        target.write_bytes(binary)
        target.chmod(0o755)
        print(f"Installed {name} {version} ({host}), pinned SHA256 verified")
    schema = download("https://raw.githubusercontent.com/spdx/spdx-spec/v2.3/schemas/spdx-schema.json",
                      "239208b7ac287b3cf5d9a9af23f9d69863971102a5e1587a27a398b43490b89b")
    (destination / "spdx-schema.json").write_bytes(schema)


if __name__ == "__main__":
    main()
