package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

const (
	upgradeRepo        = "gridctl/gridctl"
	upgradeReleaseAPI  = "https://api.github.com/repos/" + upgradeRepo + "/releases?per_page=1"
	upgradeHTTPTimeout = 60 * time.Second
	// Hard caps for downloaded artifacts. Releases are ~60 MiB today; the
	// archive cap leaves generous headroom while still bounding malicious
	// streams. checksums.txt is normally <1 KiB.
	upgradeMaxArchiveSize   = 256 << 20
	upgradeMaxChecksumsSize = 64 << 10
)

var (
	upgradeCheck   bool
	upgradeVersion string
	upgradeForce   bool
	upgradeYes     bool
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade gridctl to the latest release",
	Long: `Checks for a newer gridctl release on GitHub and replaces the running
binary in place after verifying its SHA256 checksum.

If gridctl was installed via Homebrew, this command defers to
'brew upgrade gridctl/tap/gridctl' and exits.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runUpgrade()
	},
}

func init() {
	upgradeCmd.Flags().BoolVar(&upgradeCheck, "check", false, "Only check for updates; do not install")
	upgradeCmd.Flags().StringVar(&upgradeVersion, "version", "", "Install a specific release tag (allows downgrades)")
	upgradeCmd.Flags().BoolVar(&upgradeForce, "force", false, "Bypass Homebrew detection and the up-to-date short-circuit")
	upgradeCmd.Flags().BoolVarP(&upgradeYes, "yes", "y", false, "Non-interactive: skip the confirmation prompt")
	rootCmd.AddCommand(upgradeCmd)
}

func runUpgrade() error {
	// Resolve the running binary's absolute path so we can detect brew + replace it.
	exePath, err := resolveExecutable()
	if err != nil {
		return fmt.Errorf("resolving executable path: %w", err)
	}

	// Brew detection — defer to `brew upgrade` unless --force.
	if isHomebrewPath(exePath) && !upgradeForce {
		fmt.Printf("gridctl is installed via Homebrew at %s\n", exePath)
		fmt.Println("Run `brew upgrade gridctl/tap/gridctl` to update.")
		return nil
	}

	// Resolve target version.
	currentTag := normalizeTag(version)
	var targetTag string
	if upgradeVersion != "" {
		targetTag = normalizeTag(upgradeVersion)
	} else {
		latest, err := fetchLatestTag()
		if err != nil {
			return err
		}
		targetTag = normalizeTag(latest)
	}

	targetLabel := "Latest version: "
	if upgradeVersion != "" {
		targetLabel = "Target version: "
	}
	fmt.Printf("Current version: %s\n", currentTag)
	fmt.Printf("%s%s\n", targetLabel, targetTag)

	// Compare. Use semver where possible; fall back to exact-string equality.
	cmp, ok := compareTags(currentTag, targetTag)
	switch {
	case upgradeVersion == "" && ok && cmp >= 0 && !upgradeForce:
		fmt.Println("gridctl is up to date.")
		return nil
	case upgradeCheck:
		if ok && cmp >= 0 {
			fmt.Println("gridctl is up to date.")
		} else {
			fmt.Println("An update is available. Run `gridctl upgrade` to update.")
		}
		return nil
	}

	// Confirm unless --yes or non-interactive. Non-interactive without --yes is
	// refused — destructive operations should not auto-run from cron / CI.
	if !upgradeYes {
		if !isatty.IsTerminal(os.Stdin.Fd()) {
			return fmt.Errorf("non-interactive shell detected; pass --yes to confirm the upgrade")
		}
		fmt.Printf("\nUpgrade gridctl to %s? [y/N] ", targetTag)
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			// proceed
		default:
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Download → verify → replace.
	tmpDir, err := os.MkdirTemp("", "gridctl-upgrade-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	versionNum := strings.TrimPrefix(targetTag, "v")
	archiveName := fmt.Sprintf("gridctl_%s_%s_%s.tar.gz", versionNum, runtime.GOOS, runtime.GOARCH)
	archiveURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", upgradeRepo, targetTag, archiveName)
	checksumsURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/checksums.txt", upgradeRepo, targetTag)

	fmt.Printf("\n  Downloading %s\n", archiveName)
	archivePath := filepath.Join(tmpDir, archiveName)
	if err := downloadFile(archiveURL, archivePath, upgradeMaxArchiveSize); err != nil {
		return fmt.Errorf("downloading release: %w", err)
	}
	checksumsPath := filepath.Join(tmpDir, "checksums.txt")
	if err := downloadFile(checksumsURL, checksumsPath, upgradeMaxChecksumsSize); err != nil {
		return fmt.Errorf("downloading checksums: %w", err)
	}

	fmt.Println("  Verifying SHA256")
	if err := verifySHA256(archivePath, checksumsPath, archiveName); err != nil {
		return err
	}
	fmt.Println("  ✓ checksum matches")

	binPath := filepath.Join(tmpDir, "gridctl")
	if err := extractGridctlBinary(archivePath, binPath); err != nil {
		return err
	}

	if err := atomicReplace(exePath, binPath); err != nil {
		return fmt.Errorf("replacing binary: %w", err)
	}

	fmt.Printf("\nUpgraded gridctl from %s to %s\n", currentTag, targetTag)
	return nil
}

// resolveExecutable returns the absolute path of the running binary,
// resolving any symlinks (matters for Homebrew shim detection).
func resolveExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe, nil // fall back to the raw path; some sandboxes block EvalSymlinks
	}
	return resolved, nil
}

// isHomebrewPath returns true if the path looks like a Homebrew-managed install.
func isHomebrewPath(p string) bool {
	lower := strings.ToLower(p)
	return strings.Contains(lower, "/cellar/") ||
		strings.Contains(lower, "/homebrew/") ||
		strings.Contains(lower, "/linuxbrew/")
}

// normalizeTag ensures the tag is prefixed with a single 'v' and trims whitespace.
func normalizeTag(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return ""
	}
	return "v" + strings.TrimPrefix(t, "v")
}

// compareTags compares two semver-shaped tags. Returns (cmp, ok) where ok=false
// indicates one of the tags wasn't parseable (e.g., the dev placeholder "v0.0.0-dev").
func compareTags(current, target string) (int, bool) {
	cur, err := semver.NewVersion(strings.TrimPrefix(current, "v"))
	if err != nil {
		return 0, false
	}
	tgt, err := semver.NewVersion(strings.TrimPrefix(target, "v"))
	if err != nil {
		return 0, false
	}
	return cur.Compare(tgt), true
}

// fetchLatestTag hits the GitHub API for the most recent release (including
// pre-releases, since gridctl is currently in beta).
//
// TODO: once a stable release ships, switch to the lighter
// `https://github.com/<repo>/releases/latest` redirect.
func fetchLatestTag() (string, error) {
	client := &http.Client{Timeout: upgradeHTTPTimeout}
	req, err := http.NewRequest(http.MethodGet, upgradeReleaseAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	// GitHub's API requires a User-Agent and silently returns an empty array
	// for bot-like defaults (e.g. Go-http-client). Identify ourselves.
	req.Header.Set("User-Agent", "gridctl/"+version)
	// Send a Bearer token when GITHUB_TOKEN is present (CI environments) to
	// bypass the unauthenticated rate limit. End users running interactively
	// stay below the unauth limit easily and don't need to set this.
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("contacting api.github.com: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("api.github.com returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, upgradeMaxChecksumsSize))
	if err != nil {
		return "", fmt.Errorf("reading release response: %w", err)
	}
	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &releases); err != nil {
		return "", fmt.Errorf("decoding release response: %w (body: %s)", err, snippet(body))
	}
	if len(releases) == 0 || releases[0].TagName == "" {
		return "", fmt.Errorf("no releases returned by api.github.com (body: %s)", snippet(body))
	}
	return releases[0].TagName, nil
}

// snippet returns the first 200 bytes of body for diagnostic error messages.
func snippet(body []byte) string {
	const max = 200
	if len(body) > max {
		return string(body[:max]) + "..."
	}
	return string(body)
}

// downloadFile streams url into dest, refusing to write more than maxBytes.
func downloadFile(url, dest string, maxBytes int64) error {
	client := &http.Client{Timeout: upgradeHTTPTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s returned %s", url, resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return err
	}
	if n > maxBytes {
		return fmt.Errorf("response exceeds %d-byte cap for %s", maxBytes, url)
	}
	return nil
}

// verifySHA256 reads checksumsPath, locates the entry for archiveName, and
// compares it against the SHA256 of archivePath.
func verifySHA256(archivePath, checksumsPath, archiveName string) error {
	expected, err := lookupChecksum(checksumsPath, archiveName)
	if err != nil {
		return err
	}
	actual, err := fileSHA256(archivePath)
	if err != nil {
		return fmt.Errorf("computing SHA256: %w", err)
	}
	if !strings.EqualFold(expected, actual) {
		return fmt.Errorf("checksum mismatch for %s\n  expected: %s\n  actual:   %s", archiveName, expected, actual)
	}
	return nil
}

func lookupChecksum(path, name string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[1] == name {
			return fields[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no checksum entry for %s", name)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractGridctlBinary pulls the `gridctl` entry out of the GoReleaser tarball
// and writes it to dest with mode 0755.
func extractGridctlBinary(archivePath, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("opening gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}
		if filepath.Base(hdr.Name) != "gridctl" || hdr.Typeflag != tar.TypeReg {
			continue
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		// Bounded copy: read one byte over the cap so we can distinguish a
		// finished binary (n <= cap) from an oversized one (n > cap).
		// SHA256 verification of the archive still runs upstream.
		n, copyErr := io.Copy(out, io.LimitReader(tr, upgradeMaxArchiveSize+1))
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if n > upgradeMaxArchiveSize {
			return fmt.Errorf("extracted binary exceeds %d-byte cap", upgradeMaxArchiveSize)
		}
		return nil
	}
	return fmt.Errorf("archive did not contain a 'gridctl' binary")
}

// atomicReplace writes src into a sibling of dst (same directory, atomic
// rename guaranteed) and renames it over the running binary. The kernel keeps
// the running process's executable open via the inode, so this is safe.
func atomicReplace(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".gridctl-new-*")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()

	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Best-effort: clear the macOS quarantine xattr so the new binary runs
	// without a Gatekeeper prompt.
	if runtime.GOOS == "darwin" {
		_ = exec.Command("xattr", "-dr", "com.apple.quarantine", tmpPath).Run()
	}

	if err := os.Rename(tmpPath, dst); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming %s to %s: %w", tmpPath, dst, err)
	}
	return nil
}
