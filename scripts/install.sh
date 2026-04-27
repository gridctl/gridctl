#!/bin/sh
# gridctl installer.
#
# Installs the latest gridctl release for macOS or Linux/WSL2 (amd64, arm64).
# Verifies the published SHA256 checksum before installing.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/gridctl/gridctl/main/install.sh | sh
#   sh install.sh                          # install latest
#   sh install.sh --uninstall              # remove the binary
#   sh install.sh --uninstall --purge      # also remove ~/.gridctl
#
# Environment variables:
#   GRIDCTL_VERSION       Pin a release tag (e.g., v0.1.0-beta.6).
#   GRIDCTL_INSTALL_DIR   Install destination (default: $HOME/.local/bin).
#   GRIDCTL_INSTALL_DEBUG When set to 1, prints verbose detection output.
#   NO_COLOR              Disable ANSI colors (https://no-color.org).

set -eu

REPO="gridctl/gridctl"
RELEASES_URL="https://github.com/${REPO}/releases"
RAW_INSTALL_URL="https://raw.githubusercontent.com/${REPO}/main/install.sh"
DEFAULT_INSTALL_DIR="${HOME}/.local/bin"
CONFIG_DIR="${HOME}/.gridctl"

FORCE=0
UNINSTALL=0
PURGE=0

# --- color + logging ---------------------------------------------------------

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
    BOLD="$(printf '\033[1m')"
    DIM="$(printf '\033[2m')"
    RED="$(printf '\033[31m')"
    GREEN="$(printf '\033[32m')"
    YELLOW="$(printf '\033[33m')"
    RESET="$(printf '\033[0m')"
else
    BOLD=""; DIM=""; RED=""; GREEN=""; YELLOW=""; RESET=""
fi

info() { printf '  %s%s%s %s\n' "$DIM" "$1" "$RESET" "$2"; }
ok()   { printf '  %s%s%s %s\n' "$GREEN" "✓" "$RESET" "$1"; }
warn() { printf '%s%s%s %s\n' "$YELLOW" "warning:" "$RESET" "$1" >&2; }
err()  { printf '%s%s%s %s\n' "$RED" "error:" "$RESET" "$1" >&2; }
debug() {
    if [ "${GRIDCTL_INSTALL_DEBUG:-0}" = "1" ]; then
        printf '  %s[debug] %s%s\n' "$DIM" "$1" "$RESET"
    fi
}

# --- helpers -----------------------------------------------------------------

require_cmd() {
    command -v "$1" >/dev/null 2>&1 || {
        err "$1 is required but not installed."
        exit 1
    }
}

# Returns 0 if the given path looks like a Homebrew-managed install.
is_brew_path() {
    case "$1" in
        */Cellar/*|*/homebrew/*|*/Homebrew/*|*/linuxbrew/*) return 0 ;;
        *) return 1 ;;
    esac
}

# --- platform detection ------------------------------------------------------

detect_platform() {
    raw_os="$(uname -s)"
    raw_arch="$(uname -m)"

    case "$raw_os" in
        Darwin) OS="darwin" ;;
        Linux)  OS="linux" ;;
        *)
            err "gridctl supports macOS and Linux."
            err "Windows is supported via WSL2 — install WSL2, then run this command inside your Linux distribution."
            exit 1
            ;;
    esac

    case "$raw_arch" in
        x86_64|amd64) ARCH="amd64" ;;
        arm64|aarch64) ARCH="arm64" ;;
        *)
            err "No release artifact for ${OS}/${raw_arch}."
            err "See ${RELEASES_URL} or build from source: https://github.com/${REPO}#build-from-source"
            exit 1
            ;;
    esac

    debug "platform: ${OS}/${ARCH} (uname: ${raw_os} ${raw_arch})"
}

# --- version resolution ------------------------------------------------------
#
# We hit the GitHub API rather than the /releases/latest redirect because
# gridctl is in pre-release (v0.1.0-beta.x) and the redirect target excludes
# pre-releases. Once a stable release ships, the simpler approach is:
#   curl -fsSLI -o /dev/null -w '%{url_effective}' \
#     https://github.com/${REPO}/releases/latest
# and parse the redirected URL for the tag.

resolve_version() {
    if [ -n "${GRIDCTL_VERSION:-}" ]; then
        TAG="${GRIDCTL_VERSION#v}"
        TAG="v${TAG}"
        debug "version pinned: ${TAG}"
    else
        api="https://api.github.com/repos/${REPO}/releases?per_page=1"
        debug "fetching latest release from ${api}"
        # GitHub's API requires a User-Agent header and silently returns an
        # empty array for bot-like defaults — identify ourselves.
        # Bearer auth is added when GITHUB_TOKEN is set (CI environments) to
        # bypass the 60-req/hr unauthenticated rate limit; interactive users
        # don't need a token.
        ua="gridctl-installer"
        if [ -n "${GITHUB_TOKEN:-}" ]; then
            body="$(curl -fsSL -A "$ua" -H "Authorization: Bearer ${GITHUB_TOKEN}" "$api" 2>/dev/null)" || body=""
        else
            body="$(curl -fsSL -A "$ua" "$api" 2>/dev/null)" || body=""
        fi
        if [ -z "$body" ]; then
            err "Could not reach api.github.com to resolve the latest version."
            err "Check your network or pin a version with GRIDCTL_VERSION=v0.1.0-beta.6."
            exit 1
        fi
        TAG="$(printf '%s\n' "$body" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)"
        if [ -z "$TAG" ]; then
            err "Could not parse the latest release tag from api.github.com."
            err "(Possible rate limit — set GITHUB_TOKEN or pin GRIDCTL_VERSION=v0.1.0-beta.6.)"
            err "See ${RELEASES_URL}."
            # Surface the first 200 chars of the response so API shape changes
            # (rate-limit JSON, auth errors) are diagnosable without re-running.
            snippet="$(printf '%s' "$body" | tr -d '\n' | cut -c1-200)"
            err "response: ${snippet}"
            exit 1
        fi
        debug "latest tag: ${TAG}"
    fi
    VERSION="${TAG#v}"
    ARCHIVE="gridctl_${VERSION}_${OS}_${ARCH}.tar.gz"
    ARCHIVE_URL="https://github.com/${REPO}/releases/download/${TAG}/${ARCHIVE}"
    CHECKSUMS_URL="https://github.com/${REPO}/releases/download/${TAG}/checksums.txt"
}

# --- existing-install detection ---------------------------------------------

check_existing_install() {
    existing="$(command -v gridctl 2>/dev/null || true)"
    [ -z "$existing" ] && return 0

    if is_brew_path "$existing"; then
        if [ "$FORCE" -eq 1 ]; then
            warn "Homebrew install detected at ${existing}; --force in effect, continuing."
            return 0
        fi
        printf '\n%sgridctl is installed via Homebrew at %s%s\n' "$BOLD" "$existing" "$RESET"
        printf 'Use %sbrew upgrade gridctl/tap/gridctl%s to update,\n' "$BOLD" "$RESET"
        printf 'or %sbrew uninstall gridctl/tap/gridctl%s and rerun this script.\n' "$BOLD" "$RESET"
        printf 'Pass %s--force%s to install over the Homebrew copy.\n' "$BOLD" "$RESET"
        exit 0
    fi

    # Idempotent re-run: same version already installed at the resolved path.
    # Pipe through cat to force non-TTY output ("gridctl <version>\n").
    if [ -x "$existing" ]; then
        installed="$("$existing" version 2>/dev/null | cat | sed -n '1s/^gridctl //p' || true)"
        installed="${installed#v}"
        if [ -n "$installed" ] && [ "$installed" = "$VERSION" ]; then
            printf '%sgridctl %s is already installed at %s%s\n' "$BOLD" "$TAG" "$existing" "$RESET"
            exit 0
        fi
    fi
}

# --- download ---------------------------------------------------------------

download() {
    tmpdir="$(mktemp -d -t gridctl-install.XXXXXX)"
    # shellcheck disable=SC2064
    trap "rm -rf '$tmpdir'" EXIT INT TERM

    info "Downloading" "$ARCHIVE"
    debug "archive URL: ${ARCHIVE_URL}"
    if ! curl -fsSI "$ARCHIVE_URL" >/dev/null 2>&1; then
        err "Release artifact not found at ${ARCHIVE_URL}."
        err "The release may not have built for ${OS}/${ARCH}. See ${RELEASES_URL}."
        exit 1
    fi
    curl -fsSL "$ARCHIVE_URL" -o "$tmpdir/$ARCHIVE"
    curl -fsSL "$CHECKSUMS_URL" -o "$tmpdir/checksums.txt"
}

verify_checksum() {
    info "Verifying" "SHA256"

    expected="$(awk -v f="$ARCHIVE" '$2 == f {print $1}' "$tmpdir/checksums.txt")"
    if [ -z "$expected" ]; then
        err "${ARCHIVE} not listed in checksums.txt — cannot verify."
        err "Please open an issue: https://github.com/${REPO}/issues"
        exit 1
    fi

    if command -v sha256sum >/dev/null 2>&1; then
        actual="$(sha256sum "$tmpdir/$ARCHIVE" | awk '{print $1}')"
    else
        # macOS without coreutils: shasum -a 256 is built in.
        actual="$(shasum -a 256 "$tmpdir/$ARCHIVE" | awk '{print $1}')"
    fi

    if [ "$expected" != "$actual" ]; then
        err "Checksum verification failed for ${ARCHIVE}."
        err "  expected: ${expected}"
        err "  actual:   ${actual}"
        err "Please open an issue: https://github.com/${REPO}/issues"
        exit 1
    fi
    ok "checksum matches"
}

install_binary() {
    install_dir="${GRIDCTL_INSTALL_DIR:-$DEFAULT_INSTALL_DIR}"
    info "Installing to" "$install_dir"

    tar -xzf "$tmpdir/$ARCHIVE" -C "$tmpdir" gridctl
    [ -f "$tmpdir/gridctl" ] || {
        err "Archive did not contain a 'gridctl' binary."
        exit 1
    }

    if ! mkdir -p "$install_dir" 2>/dev/null; then
        err "Cannot create ${install_dir}."
        err "Set a writable destination: GRIDCTL_INSTALL_DIR=\$HOME/.local/bin sh install.sh"
        exit 1
    fi
    if ! mv "$tmpdir/gridctl" "$install_dir/gridctl" 2>/dev/null; then
        err "Cannot write to ${install_dir} (permission denied)."
        err "Set a writable destination: GRIDCTL_INSTALL_DIR=\$HOME/.local/bin sh install.sh"
        exit 1
    fi
    chmod 0755 "$install_dir/gridctl"

    if [ "$OS" = "darwin" ]; then
        xattr -dr com.apple.quarantine "$install_dir/gridctl" 2>/dev/null || true
    fi

    DEST="$install_dir/gridctl"
    INSTALL_DIR_RESOLVED="$install_dir"
    ok "installed gridctl ${TAG}"
}

print_path_guidance() {
    case ":${PATH}:" in
        *":${INSTALL_DIR_RESOLVED}:"*) return 0 ;;
    esac
    printf '\n%sNote:%s %s is not on your PATH.\n' "$YELLOW" "$RESET" "$INSTALL_DIR_RESOLVED"
    printf 'Add this line to your shell profile (~/.zshrc, ~/.bashrc, ...):\n\n'
    # The literal $PATH is intentional — we want it shown verbatim to the user.
    # shellcheck disable=SC2016
    printf '  %sexport PATH="%s:$PATH"%s\n' "$BOLD" "$INSTALL_DIR_RESOLVED" "$RESET"
}

print_success() {
    printf '\n%sInstalled gridctl %s%s → %s\n' "$BOLD" "$TAG" "$RESET" "$DEST"
    printf 'Run %sgridctl --help%s to get started.\n' "$BOLD" "$RESET"
    printf 'Docs: https://github.com/%s\n' "$REPO"
}

# --- uninstall ---------------------------------------------------------------

uninstall() {
    target="$(command -v gridctl 2>/dev/null || true)"
    if [ -z "$target" ]; then
        target="${GRIDCTL_INSTALL_DIR:-$DEFAULT_INSTALL_DIR}/gridctl"
    fi

    if is_brew_path "$target"; then
        printf '%sgridctl is installed via Homebrew at %s%s\n' "$BOLD" "$target" "$RESET"
        printf 'Run %sbrew uninstall gridctl/tap/gridctl%s to remove it.\n' "$BOLD" "$RESET"
        # --purge still applies — brew won't remove ~/.gridctl.
        purge_config_if_requested
        return
    fi

    if [ -e "$target" ]; then
        rm -f "$target"
        ok "removed binary at ${target}"
    else
        printf '%sgridctl is not installed at %s.%s\n' "$DIM" "$target" "$RESET"
    fi

    purge_config_if_requested

    printf '\ngridctl has been uninstalled.\n'
    printf 'To reinstall, run:\n'
    printf '  %scurl -fsSL %s | sh%s\n' "$BOLD" "$RAW_INSTALL_URL" "$RESET"
}

purge_config_if_requested() {
    [ "$PURGE" -eq 1 ] || return 0
    if [ -d "$CONFIG_DIR" ]; then
        rm -rf "$CONFIG_DIR"
        ok "removed config directory ${CONFIG_DIR}"
    else
        printf '%sno config directory at %s to remove.%s\n' "$DIM" "$CONFIG_DIR" "$RESET"
    fi
}

# --- argument parsing --------------------------------------------------------

usage() {
    cat <<USAGE
gridctl installer

Usage:
  install.sh [--force]
  install.sh --uninstall [--purge]
  install.sh --help

Options:
  --force       Install over a Homebrew-managed gridctl.
  --uninstall   Remove the gridctl binary.
  --purge       With --uninstall, also remove ~/.gridctl.
  --help        Show this message.

Environment:
  GRIDCTL_VERSION       Pin a release tag (e.g., v0.1.0-beta.6).
  GRIDCTL_INSTALL_DIR   Install destination (default: \$HOME/.local/bin).
USAGE
}

parse_args() {
    for arg in "$@"; do
        case "$arg" in
            --force)     FORCE=1 ;;
            --uninstall) UNINSTALL=1 ;;
            --purge)     PURGE=1 ;;
            -h|--help)   usage; exit 0 ;;
            *)
                err "unknown option: $arg"
                usage >&2
                exit 1
                ;;
        esac
    done
    if [ "$PURGE" -eq 1 ] && [ "$UNINSTALL" -eq 0 ]; then
        err "--purge requires --uninstall"
        exit 1
    fi
}

# --- main --------------------------------------------------------------------

main() {
    parse_args "$@"
    require_cmd curl
    require_cmd tar
    require_cmd uname

    if [ "$UNINSTALL" -eq 1 ]; then
        uninstall
        return
    fi

    printf '\n%sgridctl%s installer\n\n' "$BOLD" "$RESET"
    detect_platform
    info "Platform" "${OS}/${ARCH}"
    resolve_version
    info "Release" "$TAG"
    check_existing_install
    download
    verify_checksum
    install_binary
    print_success
    print_path_guidance
}

main "$@"
