# Release Verification

Checksums detect corruption. Authenticated provenance also establishes that the expected workflow signed the exact archive for the expected source commit and tag. Successful verification does not establish harmlessness, reproducibility, or a SLSA level.

## Coverage

No first provenance-covered production version is assigned in advance. The first covered release must be identified in its published release notes after acceptance passes, with its full source SHA and `provenance.sigstore.json` asset. Earlier releases and local/source builds have no guarantee under this policy. Missing legacy evidence is not a reason to relax verification for a covered release.

The installer and `gridctl upgrade` still check SHA256 against the release's checksum file. Homebrew uses its cask checksum. None automatically enforces this origin policy.

## Verify Before Installing

Use GitHub CLI 2.87.3 or later, obtained through an independently trusted package manager or the CLI project's authenticated distribution. Do not bootstrap the verifier or trusted roots solely from the Gridctl bundle being checked. Release tooling pins CLI 2.87.3 and its platform download digests in `scripts/release-tools.py`.

Select a tag and independently obtain its full 40-character source SHA from reviewed release announcements and source history. Do not derive expected identities solely from an unverified bundle or inventory. Replace both placeholders below; the example does not select a production release.

```bash
tag='REPLACE_WITH_RELEASE_TAG'
sha='REPLACE_WITH_FULL_SOURCE_COMMIT_SHA'
os=linux       # darwin for macOS
arch=amd64     # arm64 for Apple Silicon or Linux ARM64
archive="gridctl_${tag#v}_${os}_${arch}.tar.gz"
base="https://github.com/gridctl/gridctl/releases/download/$tag"
curl --fail --location --output "$archive" "$base/$archive"
curl --fail --location --output provenance.sigstore.json "$base/provenance.sigstore.json"
gh attestation verify "$archive" \
  --bundle provenance.sigstore.json \
  --repo gridctl/gridctl \
  --cert-identity "https://github.com/gridctl/gridctl/.github/workflows/release.yaml@refs/tags/$tag" \
  --source-ref "refs/tags/$tag" \
  --source-digest "$sha" \
  --cert-oidc-issuer https://token.actions.githubusercontent.com \
  --predicate-type https://slsa.dev/provenance/v1 \
  --deny-self-hosted-runners
```

The signer is `release.yaml`, not the reusable Gatekeeper validation workflow. Building and signing occur in the same job. In `uname -m` output, `x86_64` means `amd64`, and `aarch64` or `arm64` means `arm64`.

Local `--bundle` verification requires no GitHub account or login. Trust-root retrieval can require networking. Fully offline operation is not claimed; disconnected, empty-cache verification with independently staged roots is a separate requirement.

Only after successful verification, extract and install those same local bytes:

```bash
mkdir verified-gridctl
tar -xzf "$archive" -C verified-gridctl
mkdir -p "$HOME/.local/bin"
install -m 0755 verified-gridctl/gridctl "$HOME/.local/bin/gridctl"
```

Do not invoke the installer or updater to download again. Do not overwrite Homebrew-managed files; use a separate standalone path or remain in Homebrew's update flow.

## Failures

- Missing bundle: check legacy coverage. Checksums are not equivalent origin evidence.
- Wrong repository, workflow, SHA, tag, issuer, or predicate: stop and check independently obtained expected values. Never broaden accepted identity to bypass a mismatch.
- Changed bytes or invalid signature: discard the archive and investigate before retrying.
- Unavailable verifier: install an independently trusted verifier first. Do not execute Gridctl to verify itself.
- Network, trust-root, or authentication error: verification infrastructure failed. Preserve the diagnostic and resolve it before retrying; this is not a successful cryptographic check. Local bundle mode should not require login.

## Inventory Assets

`release-inventory.json` is the readable index recording inventory scope, generator version, archive relationships, and digests. Each archive has a Syft 1.42.0 SPDX 2.3 document named `<archive>.spdx.json`, describing Go components cataloged from its final bytes.

`frontend-build.spdx.json` inventories the installed frontend build tree using npm 11.10.1 and SPDX 2.3, including development tools such as Vite. `--legacy-peer-deps` accommodates transitive packages that declare older React peer ranges; package inventory is not a claim that every peer declaration is satisfied. The npm audit gate remains unchanged. This is not exact browser-bundle attribution or a merged Go/frontend SBOM.

The four archives, five SPDX documents, index, generated cask, and `checksums.txt` are direct provenance subjects. Verify an inventory by substituting its filename in the command above while preserving expected identity and source constraints. Check index digests only after authenticating the index. Bundles are generated last and excluded from their own authenticated checksums to avoid circularity.

## Maintainer Operations

All six exact-commit Gatekeeper jobs are required: backend test/lint/coverage/vulnerability/build/example policies, frontend tests/lint/types/audit/build, Docker integration, Podman integration, LiteLLM contract, and MCP conformance. Failed, missing, canceled, and skipped results fail closed. Prior branch or PR runs do not substitute.

GoReleaser OSS 2.14.3 builds archives and inventories into a draft. Its tap upload is disabled; the generated cask is authenticated with the assets. Linux and macOS jobs verify downloaded draft bytes and rejection cases. Publication checks the tag again and reverifies the draft. The tap advances after public asset download and provenance checks. GoReleaser alone is not an alternative production publication path.

GitHub draft assets require write-capable repository access. The assembly job reads them back and supplies a same-run Actions artifact to read-only Linux/macOS verifier jobs. These jobs authenticate every expected file independently against the repository/workflow/tag/SHA policy, so the transport is not their origin trust anchor. No PR artifacts are reused. The publisher separately downloads and verifies the current draft before publication.

`RELEASE_MODE` defaults to `mutable`, preserving the repository's current setting. To declare `immutable`, a maintainer must separately enable GitHub immutable releases and arrange Administration read access for the settings preflight. Workflows never change repository settings. Absent, disabled, unreadable, or malformed prerequisites stop immutable publication. The default workflow token may lack Administration read; never broaden or repurpose the tap-only token to solve this.

Before publication, an authorized maintainer may repair or remove an unpublished draft after reviewing its source and assets, then rerun complete validation. After publication, never replace assets or move the tag; issue a new version. If only the tap update fails, reverify public assets before retrying the authenticated cask update. Do not rebuild the release.

Release maintainers own executable, npm, Python validation, scanner, and schema pins. Actions pins receive weekly Dependabot updates. Each update requires upstream release/license review, independent digest verification, local policy/schema and GoReleaser checks, and hosted Linux/macOS acceptance. GitHub CLI and actions use MIT, Syft uses Apache-2.0, and npm uses Artistic-2.0. Hosted runner labels are not immutable environment pins.
