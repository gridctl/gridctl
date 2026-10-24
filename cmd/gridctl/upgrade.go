package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
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
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/versions"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

const (
	upgradeRepo         = "gridctl/gridctl"
	upgradeReleaseAPI   = "https://api.github.com/repos/" + upgradeRepo + "/releases?per_page=1"
	upgradeDownloadBase = "https://github.com/" + upgradeRepo + "/releases/download"
	upgradeHTTPTimeout  = 60 * time.Second
	// Hard caps for downloaded artifacts. Releases are ~60 MiB today; the
	// archive cap leaves generous headroom while still bounding malicious
	// streams. checksums.txt is normally <1 KiB.
	upgradeMaxArchiveSize   = 256 << 20
	upgradeMaxChecksumsSize = 64 << 10

	// rollbackPrevious is the NoOptDefVal for --rollback, meaning the
	// manifest's previous tag.
	rollbackPrevious = "previous"

	// Test-only overrides. Not a supported configuration: a standalone
	// upgrade always uses the GitHub URLs above unless a test sets these.
	envUpgradeReleaseAPI   = "GRIDCTL_UPGRADE_RELEASE_API"
	envUpgradeDownloadBase = "GRIDCTL_UPGRADE_DOWNLOAD_BASE"
)

var (
	upgradeCheck    bool
	upgradeVersion  string
	upgradeForce    bool
	upgradeYes      bool
	upgradeRollback string
	upgradeList     bool
	upgradeKeep     int
	upgradeFormat   string
	upgradeRestart  bool
)

var errUpgradeAborted = errors.New("upgrade aborted")

// upgradeExit is returned for the upgrade command's own exit 2 contract.
// RunE prints the message and calls os.Exit so Cobra's error path (exit 1)
// is not used. Message text is already written to the upgrader's stderr.
type upgradeExit struct {
	Code int
	msg  string
}

func (e *upgradeExit) Error() string {
	if e == nil {
		return ""
	}
	return e.msg
}

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade gridctl to the latest release",
	Long: `Checks for a newer gridctl release on GitHub and replaces the running
binary in place after verifying its SHA256 checksum.

A standalone install keeps the replaced binary and the installed binary
under ~/.gridctl/versions (default 2). --keep <n> is the count for this
run, not a stored setting. Active and previous are always kept, so
--keep 1 still leaves both. --keep 0 disables retention for this run.
--rollback restores a retained binary offline. --list prints the store.
A dev build is not retained. Each retained binary records the schema
versions it advertises. --rollback refuses, before the confirmation
prompt, when daemon state, pin files, the project lock, the import lock,
or a stack or pack floor is newer than the target reads. --force turns
those refusals into warnings and continues.

After a successful upgrade or rollback, running daemons are listed with
a restart command. --restart performs it. A service-owned daemon is
restarted through its supervisor. A plain daemon is destroy plus apply
--port, plus --override when status reports one. The stackless daemon is
destroy plus serve --port. Flags that were not recorded are named and
are not reproduced. On macOS the service line is gridctl upgrade
--restart and the launchd label.

If gridctl was installed via Homebrew, this command defers to
'brew upgrade gridctl/tap/gridctl' and exits. Rollback of a Homebrew
install is not supported unless --force. Rollback is not supported on
Windows.

Exit codes: 0 success, including a --force rollback that printed
warnings; 1 a failed download, verification, swap, or restart; 2 nothing
to do as asked (nothing retained, the named tag is absent, or a pre-swap
refusal).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		u := newUpgraderFromFlags(cmd)
		// pflag does not consume the next argument of a NoOptDefVal flag
		// unless it is attached with '='. `--rollback v1.0.0` therefore
		// arrives as a positional. `--rollback=v1.0.0` is already on the flag.
		if u.rollback && u.rollbackTag == "" && len(args) > 0 {
			if len(args) != 1 {
				return fmt.Errorf("rollback accepts at most one tag")
			}
			u.rollbackTag = args[0]
		}
		err := u.run(cmd.Context())
		var code *upgradeExit
		if errors.As(err, &code) {
			os.Exit(code.Code)
		}
		return err
	},
}

func init() {
	upgradeCmd.Flags().BoolVar(&upgradeCheck, "check", false, "Only check for updates; do not install")
	upgradeCmd.Flags().StringVar(&upgradeVersion, "version", "", "Install a specific release tag (allows downgrades)")
	upgradeCmd.Flags().BoolVar(&upgradeForce, "force", false, "Bypass Homebrew detection, the up-to-date short-circuit, and rollback compatibility refusals")
	upgradeCmd.Flags().BoolVarP(&upgradeYes, "yes", "y", false, "Non-interactive: skip the confirmation prompt")
	upgradeCmd.Flags().StringVar(&upgradeRollback, "rollback", "", "Roll back to the previous retained binary, or to the named tag")
	upgradeCmd.Flags().Lookup("rollback").NoOptDefVal = rollbackPrevious
	upgradeCmd.Flags().BoolVar(&upgradeList, "list", false, "List retained binaries and exit")
	upgradeCmd.Flags().IntVar(&upgradeKeep, "keep", 2, "Retained binaries to keep for this run (active and previous are always kept; 0 disables retention)")
	upgradeCmd.Flags().StringVar(&upgradeFormat, "format", "text", "Output format for --list: text or json")
	upgradeCmd.Flags().BoolVar(&upgradeRestart, "restart", false, "Restart running daemons after a successful upgrade or rollback")
	upgradeCmd.MarkFlagsMutuallyExclusive("rollback", "version")
	upgradeCmd.MarkFlagsMutuallyExclusive("restart", "list")
	upgradeCmd.MarkFlagsMutuallyExclusive("restart", "check")
	upgradeCmd.MarkFlagsMutuallyExclusive("list", "rollback")
	upgradeCmd.MarkFlagsMutuallyExclusive("list", "version")
	upgradeCmd.MarkFlagsMutuallyExclusive("list", "check")
}

// upgrader is the testable upgrade flow. Production wiring lives in
// newUpgrader; tests set the fields and call run.
type upgrader struct {
	resolveExe   func() (string, error)
	releaseAPI   string
	downloadBase string
	client       *http.Client
	stdin        io.Reader
	stdout       io.Writer
	stderr       io.Writer
	isTTY        func() bool
	storeDir     string
	goos         string
	version      string

	check       bool
	versionFlag string
	force       bool
	yes         bool
	rollback    bool
	rollbackTag string
	list        bool
	keep        int
	format      string
	restart     bool
	// home overrides the resolved gridctl home. Empty uses state.Home().
	// Tests set it so a rollback check does not read the real state dir.
	home string
}

func newUpgrader() *upgrader {
	return &upgrader{
		resolveExe:   resolveExecutable,
		releaseAPI:   upgradeReleaseAPI,
		downloadBase: upgradeDownloadBase,
		client:       &http.Client{Timeout: upgradeHTTPTimeout},
		stdin:        os.Stdin,
		stdout:       os.Stdout,
		stderr:       os.Stderr,
		isTTY:        func() bool { return isatty.IsTerminal(os.Stdin.Fd()) },
		goos:         runtime.GOOS,
		version:      version,
		keep:         2,
		format:       "text",
	}
}

func newUpgraderFromFlags(cmd *cobra.Command) *upgrader {
	u := newUpgrader()
	u.check = upgradeCheck
	u.versionFlag = upgradeVersion
	u.force = upgradeForce
	u.yes = upgradeYes
	u.keep = upgradeKeep
	u.format = upgradeFormat
	u.list = upgradeList
	u.restart = upgradeRestart
	if cmd != nil && cmd.Flags().Changed("rollback") {
		u.rollback = true
		if upgradeRollback != "" && upgradeRollback != rollbackPrevious {
			u.rollbackTag = upgradeRollback
		}
	}
	// Test seams. Unset in production, so the GitHub URLs stand.
	if api := os.Getenv(envUpgradeReleaseAPI); api != "" {
		u.releaseAPI = api
	}
	if base := os.Getenv(envUpgradeDownloadBase); base != "" {
		u.downloadBase = base
	}
	return u
}

func (u *upgrader) run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	u.applyDefaults()
	if u.format != "text" && !strings.EqualFold(u.format, "json") {
		return fmt.Errorf("invalid --format %q (want text or json)", u.format)
	}
	if u.keep < 0 {
		return fmt.Errorf("--keep must be >= 0")
	}
	if u.rollback && u.versionFlag != "" {
		return fmt.Errorf("cannot combine --rollback with --version")
	}

	exePath, err := u.resolveExe()
	if err != nil {
		return fmt.Errorf("resolving executable path: %w", err)
	}
	if isHomebrewPath(exePath) && !u.force {
		fmt.Fprintf(u.stdout, "gridctl is installed via Homebrew at %s\n", exePath)
		fmt.Fprintln(u.stdout, "Run `brew upgrade gridctl/tap/gridctl` to update.")
		return nil
	}
	if u.list {
		return u.listRetained(ctx)
	}
	if u.rollback {
		return u.rollbackTo(ctx, exePath)
	}
	return u.upgrade(ctx, exePath)
}

func (u *upgrader) applyDefaults() {
	if u.resolveExe == nil {
		u.resolveExe = resolveExecutable
	}
	if u.releaseAPI == "" {
		u.releaseAPI = upgradeReleaseAPI
	}
	if u.downloadBase == "" {
		u.downloadBase = upgradeDownloadBase
	}
	if u.client == nil {
		u.client = &http.Client{Timeout: upgradeHTTPTimeout}
	}
	if u.stdin == nil {
		u.stdin = strings.NewReader("")
	}
	if u.stdout == nil {
		u.stdout = io.Discard
	}
	if u.stderr == nil {
		u.stderr = io.Discard
	}
	if u.isTTY == nil {
		u.isTTY = func() bool { return false }
	}
	if u.goos == "" {
		u.goos = runtime.GOOS
	}
	if u.format == "" {
		u.format = "text"
	}
}

func (u *upgrader) upgrade(ctx context.Context, exePath string) error {
	currentTag := normalizeTag(u.version)
	var targetTag string
	if u.versionFlag != "" {
		targetTag = normalizeTag(u.versionFlag)
	} else {
		latest, err := u.fetchLatestTag(ctx)
		if err != nil {
			return err
		}
		targetTag = normalizeTag(latest)
	}
	if err := versions.ValidateTag(targetTag); err != nil {
		return fmt.Errorf("target version %q: %w", targetTag, err)
	}

	targetLabel := "Latest version: "
	if u.versionFlag != "" {
		targetLabel = "Target version: "
	}
	fmt.Fprintf(u.stdout, "Current version: %s\n", currentTag)
	fmt.Fprintf(u.stdout, "%s%s\n", targetLabel, targetTag)

	cmp, ok := compareTags(currentTag, targetTag)
	switch {
	case u.versionFlag == "" && ok && cmp >= 0 && !u.force:
		fmt.Fprintln(u.stdout, "gridctl is up to date.")
		return nil
	case u.check:
		if ok && cmp >= 0 {
			fmt.Fprintln(u.stdout, "gridctl is up to date.")
		} else {
			fmt.Fprintln(u.stdout, "An update is available. Run `gridctl upgrade` to update.")
		}
		return nil
	}

	if err := u.confirm(
		fmt.Sprintf("\nUpgrade gridctl to %s? [y/N] ", targetTag),
		"non-interactive shell detected; pass --yes to confirm the upgrade",
	); err != nil {
		if errors.Is(err, errUpgradeAborted) {
			return nil
		}
		return err
	}

	keep := u.keep
	if u.goos == "windows" && keep != 0 {
		fmt.Fprintln(u.stderr, "retention is not supported on Windows")
		keep = 0
	}

	tmpDir, err := os.MkdirTemp("", "gridctl-upgrade-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := u.openStore(ctx, false)
	if err != nil {
		return err
	}
	if store == nil && keep != 0 {
		store, err = u.openStore(ctx, true)
		if err != nil {
			return err
		}
	}
	if store != nil {
		defer store.Close()
		store.AllowSources(exePath, tmpDir)
	}

	binPath, fromStore, err := u.resolveInstallBinary(ctx, store, targetTag, tmpDir)
	if err != nil {
		return err
	}

	retainedCurrent := false
	// A same-tag reinstall must install the retained bytes, not copy the
	// running file over that store entry first. Previous stays as recorded.
	if keep != 0 && store != nil && retainableTag(currentTag) && currentTag != targetTag {
		if err := store.Retain(ctx, currentTag, exePath, versions.OriginReplaced, currentSchemas()); err != nil {
			return fmt.Errorf("retaining %s: %w", currentTag, err)
		}
		retainedCurrent = true
	}
	if keep != 0 && store != nil && !fromStore {
		schemas := u.schemasForDownloaded(ctx, binPath)
		if err := store.Retain(ctx, targetTag, binPath, versions.OriginDownloaded, schemas); err != nil {
			return fmt.Errorf("retaining %s: %w", targetTag, err)
		}
	}

	if err := atomicReplace(exePath, binPath); err != nil {
		return fmt.Errorf("replacing binary: %w", err)
	}
	if keep != 0 && store != nil {
		previous := store.Previous()
		// A same-version reinstall did not leave a different binary
		// behind. Keep the recorded previous so --rollback still has a target.
		if retainedCurrent && currentTag != targetTag {
			previous = currentTag
		}
		if err := store.SetActive(targetTag, previous); err != nil {
			return err
		}
		if _, err := store.Prune(ctx, keep); err != nil {
			return fmt.Errorf("pruning retained binaries: %w", err)
		}
	}

	fmt.Fprintf(u.stdout, "\nUpgraded gridctl from %s to %s\n", currentTag, targetTag)
	if retainedCurrent {
		fmt.Fprintf(u.stdout, "Retained %s (roll back with: gridctl upgrade --rollback)\n", currentTag)
	}
	return u.reportDaemons(ctx, exePath)
}

// schemasForDownloaded probes a downloaded or stored binary. A probe
// failure records nil so retention still proceeds; rollback then warns
// that compatibility is unknown.
func (u *upgrader) schemasForDownloaded(ctx context.Context, path string) *versions.Schemas {
	home, err := u.gridHome()
	if err != nil {
		return nil
	}
	return probeBinarySchemas(ctx, path, home)
}

// resolveInstallBinary uses a verified store entry when one exists.
// A missing entry or a checksum failure falls through to a download.
func (u *upgrader) resolveInstallBinary(ctx context.Context, store *versions.Store, targetTag, tmpDir string) (string, bool, error) {
	if store != nil {
		if _, path, ok := store.Lookup(targetTag); ok {
			if err := store.Verify(ctx, targetTag); err == nil {
				fmt.Fprintf(u.stdout, "Using retained %s (no download)\n", targetTag)
				return path, true, nil
			}
		}
	}

	versionNum := strings.TrimPrefix(targetTag, "v")
	archiveName := fmt.Sprintf("gridctl_%s_%s_%s.tar.gz", versionNum, runtime.GOOS, runtime.GOARCH)
	archiveURL := u.releaseFileURL(targetTag, archiveName)
	checksumsURL := u.releaseFileURL(targetTag, "checksums.txt")

	fmt.Fprintf(u.stdout, "\n  Downloading %s\n", archiveName)
	archivePath := filepath.Join(tmpDir, archiveName)
	if err := u.downloadFile(ctx, archiveURL, archivePath, upgradeMaxArchiveSize); err != nil {
		return "", false, fmt.Errorf("downloading release: %w", err)
	}
	checksumsPath := filepath.Join(tmpDir, "checksums.txt")
	if err := u.downloadFile(ctx, checksumsURL, checksumsPath, upgradeMaxChecksumsSize); err != nil {
		return "", false, fmt.Errorf("downloading checksums: %w", err)
	}

	fmt.Fprintln(u.stdout, "  Verifying SHA256")
	if err := verifySHA256(archivePath, checksumsPath, archiveName); err != nil {
		return "", false, err
	}
	fmt.Fprintln(u.stdout, "  ✓ checksum matches")

	binPath := filepath.Join(tmpDir, "gridctl")
	if err := extractGridctlBinary(archivePath, binPath); err != nil {
		return "", false, err
	}
	return binPath, false, nil
}

func (u *upgrader) confirm(prompt, nonInteractive string) error {
	if u.yes {
		return nil
	}
	if !u.isTTY() {
		return fmt.Errorf("%s", nonInteractive)
	}
	fmt.Fprint(u.stdout, prompt)
	reader := bufio.NewReader(u.stdin)
	line, _ := reader.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return nil
	default:
		fmt.Fprintln(u.stdout, "Aborted.")
		return errUpgradeAborted
	}
}

func (u *upgrader) versionsDir() (string, error) {
	if u.storeDir != "" {
		return u.storeDir, nil
	}
	return state.VersionsDir()
}

// openStore locks the versions directory. create false returns (nil, nil)
// when the directory does not exist yet, so a read-only caller does not
// create it. Windows never opens the store.
func (u *upgrader) openStore(ctx context.Context, create bool) (*versions.Store, error) {
	if u.goos == "windows" {
		return nil, nil
	}
	dir, err := u.versionsDir()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		if !create {
			return nil, nil
		}
	} else if err != nil {
		return nil, err
	}
	return versions.Open(ctx, dir)
}

func (u *upgrader) releaseFileURL(tag, name string) string {
	return strings.TrimRight(u.downloadBase, "/") + "/" + tag + "/" + name
}

func (u *upgrader) nothing(msg string) error {
	fmt.Fprintln(u.stderr, msg)
	return &upgradeExit{Code: 2, msg: msg}
}

// retainableTag reports whether tag is a release version safe to use as a
// store directory. Dev builds and other non-semver strings are not retained.
func retainableTag(tag string) bool {
	if versions.ValidateTag(tag) != nil {
		return false
	}
	_, err := semver.NewVersion(strings.TrimPrefix(tag, "v"))
	return err == nil
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
func (u *upgrader) fetchLatestTag(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.releaseAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	// GitHub's API requires a User-Agent and silently returns an empty array
	// for bot-like defaults (e.g. Go-http-client). Identify ourselves.
	req.Header.Set("User-Agent", "gridctl/"+u.version)
	// Send a Bearer token when GITHUB_TOKEN is present (CI environments) to
	// bypass the unauthenticated rate limit. End users running interactively
	// stay below the unauth limit easily and don't need to set this.
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := u.client.Do(req)
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
func (u *upgrader) downloadFile(ctx context.Context, url, dest string, maxBytes int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := u.client.Do(req)
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
