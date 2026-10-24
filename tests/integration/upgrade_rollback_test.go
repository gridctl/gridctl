//go:build integration && !windows

package integration

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
)

// TestUpgradeRollback_RoundTrip builds two race-enabled binaries, upgrades
// the older one through a local release server, and rolls back offline.
// GRIDCTL_UPGRADE_RELEASE_API and GRIDCTL_UPGRADE_DOWNLOAD_BASE are test
// seams read by the upgrade command; they are not a supported configuration.
func TestUpgradeRollback_RoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	oldBin := buildVersioned(ctx, t, root, dir, "gridctl-old", "v9.0.1")
	newBin := buildVersioned(ctx, t, root, dir, "gridctl-new", "v9.0.2")

	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runUpgrade(ctx, t, oldBin, home, "", "", "upgrade", "--rollback", "--yes")
	if code != 2 {
		t.Fatalf("empty rollback exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "nothing retained; run gridctl upgrade first") {
		t.Fatalf("stderr = %s", stderr)
	}
	// A store with no entries is distinct from a missing store: the named
	// tag is absent, rather than "nothing retained".
	versionsDir := filepath.Join(home, ".gridctl", "versions")
	if err := os.MkdirAll(versionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionsDir, "manifest.json"), []byte("{\"schema_version\":1,\"active\":\"\",\"previous\":\"\",\"entries\":[]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runUpgrade(ctx, t, oldBin, home, "", "", "upgrade", "--rollback", "v0.9.0", "--yes")
	if code != 2 {
		t.Fatalf("absent tag exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "v0.9.0 is not retained") {
		t.Fatalf("stderr = %s", stderr)
	}

	payload, err := os.ReadFile(newBin)
	if err != nil {
		t.Fatal(err)
	}
	var hits hitLog
	srv := httptest.NewServer(releaseHandler(t, "v9.0.2", payload, &hits))
	var closeOnce sync.Once
	closeSrv := func() { closeOnce.Do(srv.Close) }
	t.Cleanup(closeSrv)
	api := srv.URL + "/repos/gridctl/gridctl/releases?per_page=1"
	base := srv.URL + "/gridctl/gridctl/releases/download"

	stdout, stderr, code = runUpgrade(ctx, t, oldBin, home, api, base, "upgrade", "--yes", "--version", "v9.0.2")
	if code != 0 {
		t.Fatalf("upgrade exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Upgraded gridctl from v9.0.1 to v9.0.2") {
		t.Fatalf("stdout = %s", stdout)
	}
	if !strings.Contains(stdout, "Retained v9.0.1") {
		t.Fatalf("stdout = %s", stdout)
	}
	assertVersion(ctx, t, oldBin, home, "v9.0.2")
	assertManifest(t, home, "v9.0.2", "v9.0.1")

	before := hits.paths()
	if !containsPath(before, "/releases/download/") {
		t.Fatalf("first upgrade did not download: %v", before)
	}
	hits.reset()
	stdout, stderr, code = runUpgrade(ctx, t, oldBin, home, api, base, "upgrade", "--yes", "--version", "v9.0.2")
	if code != 0 {
		t.Fatalf("store hit exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Using retained v9.0.2 (no download)") {
		t.Fatalf("stdout = %s", stdout)
	}
	for _, path := range hits.paths() {
		if strings.Contains(path, "/releases/download/") {
			t.Fatalf("store hit downloaded %s", path)
		}
	}

	closeSrv()
	stdout, stderr, code = runUpgrade(ctx, t, oldBin, home, api, base, "upgrade", "--rollback", "--yes")
	if code != 0 {
		t.Fatalf("rollback exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Rolled back gridctl from v9.0.2 to v9.0.1") {
		t.Fatalf("stdout = %s", stdout)
	}
	// The replaced binary is retained with this release's schema advertisement,
	// so a rollback to it is not the unknown-schemas path.
	if strings.Contains(stderr, "compatibility unknown") {
		t.Fatalf("stderr = %s", stderr)
	}
	assertVersion(ctx, t, oldBin, home, "v9.0.1")
	assertManifest(t, home, "v9.0.1", "v9.0.2")
	assertRetainedSchemas(t, home, "v9.0.1")
}

// TestUpgradeRollback_StateAware starts a local-process daemon on the older
// binary, upgrades it, and checks status, rollback --restart, and the two
// hand-bumped refusal files. The children are race-built with distinct
// ldflags versions. No container runtime is required.
func TestUpgradeRollback_StateAware(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if mockStdioBin == "" {
		t.Fatal("mock stdio server was not built")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	oldBin := buildVersioned(ctx, t, root, dir, "gridctl-old", "v9.0.1")
	newBin := buildVersioned(ctx, t, root, dir, "gridctl-new", "v9.0.2")
	// Each subtest installs over its own copy. A shared path would stay on
	// v9.0.2 after a refused rollback, and the next upgrade would not retain
	// a previous tag.
	pristine, err := os.ReadFile(oldBin)
	if err != nil {
		t.Fatal(err)
	}
	copyOld := func(t *testing.T, home string) string {
		t.Helper()
		bin := filepath.Join(home, "gridctl")
		if err := os.WriteFile(bin, pristine, 0o755); err != nil {
			t.Fatal(err)
		}
		return bin
	}

	t.Run("restart", func(t *testing.T) {
		home := filepath.Join(dir, "restart-home")
		if err := os.MkdirAll(home, 0o755); err != nil {
			t.Fatal(err)
		}
		oldBin := copyOld(t, home)
		t.Setenv("HOME", home)
		t.Setenv("GRIDCTL_HOME", home)
		stack := filepath.Join(dir, "lab.yaml")
		body := fmt.Sprintf("version: \"1\"\nname: lab\nmcp-servers:\n  - name: echo\n    command: [%q]\n", mockStdioBin)
		if err := os.WriteFile(stack, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		port := freePort(t)
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			_, _, _ = runUpgrade(cleanupCtx, t, oldBin, home, "", "", "destroy", "lab")
		})

		stdout, stderr, code := runUpgrade(ctx, t, oldBin, home, "", "", "apply", stack, "--port", strconv.Itoa(port))
		if code != 0 {
			t.Fatalf("apply exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
		}
		st, err := state.Load("lab")
		if err != nil {
			t.Fatal(err)
		}
		if st.BinaryVersion != "v9.0.1" {
			t.Fatalf("binary_version = %q", st.BinaryVersion)
		}
		if !state.IsRunning(st) {
			t.Fatal("daemon is not running")
		}
		firstPID := st.PID

		payload, err := os.ReadFile(newBin)
		if err != nil {
			t.Fatal(err)
		}
		var hits hitLog
		srv := httptest.NewServer(releaseHandler(t, "v9.0.2", payload, &hits))
		defer srv.Close()
		api := srv.URL + "/repos/gridctl/gridctl/releases?per_page=1"
		base := srv.URL + "/gridctl/gridctl/releases/download"
		stdout, stderr, code = runUpgrade(ctx, t, oldBin, home, api, base, "upgrade", "--yes", "--version", "v9.0.2")
		if code != 0 {
			t.Fatalf("upgrade exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
		}
		assertVersion(ctx, t, oldBin, home, "v9.0.2")

		stdout, stderr, code = runUpgrade(ctx, t, oldBin, home, "", "", "status", "--plain")
		if code != 0 {
			t.Fatalf("status exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
		}
		if !strings.Contains(stderr, "warning: lab runs gridctl v9.0.1; this CLI is v9.0.2") {
			t.Fatalf("stderr = %s\nstdout: %s", stderr, stdout)
		}
		stdout, stderr, code = runUpgrade(ctx, t, oldBin, home, "", "", "status", "--json")
		if code != 0 {
			t.Fatalf("status json exit = %d\n%s\n%s", code, stdout, stderr)
		}
		if !strings.Contains(stdout, `"binary_version": "v9.0.1"`) && !strings.Contains(stdout, `"binary_version":"v9.0.1"`) {
			t.Fatalf("status json = %s", stdout)
		}
		if strings.Contains(stderr, "runs gridctl") {
			t.Fatalf("json status printed a version warning: %s", stderr)
		}

		stdout, stderr, code = runUpgrade(ctx, t, oldBin, home, "", "", "upgrade", "--rollback", "--restart", "--yes")
		if code != 0 {
			t.Fatalf("rollback --restart exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
		}
		if !strings.Contains(stdout, "Restarted lab") {
			t.Fatalf("stdout = %s\nstderr: %s", stdout, stderr)
		}
		assertVersion(ctx, t, oldBin, home, "v9.0.1")
		st, err = state.Load("lab")
		if err != nil {
			t.Fatal(err)
		}
		if st.BinaryVersion != "v9.0.1" {
			t.Fatalf("restarted binary_version = %q", st.BinaryVersion)
		}
		if st.PID == firstPID || !state.IsRunning(st) {
			t.Fatalf("daemon was not replaced: pid %d running %v", st.PID, state.IsRunning(st))
		}
	})

	t.Run("project lock", func(t *testing.T) {
		home := filepath.Join(dir, "lock-home")
		if err := os.MkdirAll(home, 0o755); err != nil {
			t.Fatal(err)
		}
		bin := copyOld(t, home)
		upgradePair(ctx, t, bin, newBin, home)
		lock := filepath.Join(home, ".gridctl", "project.lock.yaml")
		if err := os.WriteFile(lock, []byte("version: 99\nprojections: []\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := runUpgrade(ctx, t, oldBin, home, "", "", "upgrade", "--rollback", "--yes")
		if code != 2 {
			t.Fatalf("lock refusal exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
		}
		if !strings.Contains(stderr, "project.lock.yaml is version 99") || !strings.Contains(stderr, "Rollback to v9.0.1 refused:") {
			t.Fatalf("stderr = %s", stderr)
		}
		assertVersion(ctx, t, bin, home, "v9.0.2")
	})

	t.Run("tool pin", func(t *testing.T) {
		home := filepath.Join(dir, "pin-home")
		if err := os.MkdirAll(home, 0o755); err != nil {
			t.Fatal(err)
		}
		bin := copyOld(t, home)
		upgradePair(ctx, t, bin, newBin, home)
		// The pin check follows stack names in daemon state files. A
		// hand-written state file is enough; the process does not need to
		// be running.
		stateDir := filepath.Join(home, ".gridctl", "state")
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stateDir, "lab.json"), []byte("{\"schema_version\":2,\"stack_name\":\"lab\",\"stack_file\":\"\",\"pid\":0}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		pinDir := filepath.Join(home, ".gridctl", "pins")
		if err := os.MkdirAll(pinDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pinDir, "lab.json"), []byte("{\"version\":\"9\"}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := runUpgrade(ctx, t, oldBin, home, "", "", "upgrade", "--rollback", "--yes")
		if code != 2 {
			t.Fatalf("pin refusal exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
		}
		if !strings.Contains(stderr, "pins/lab.json is version 9") || !strings.Contains(stderr, "Rollback to v9.0.1 refused:") {
			t.Fatalf("stderr = %s", stderr)
		}
		assertVersion(ctx, t, bin, home, "v9.0.2")
	})
}

// upgradePair installs newBin over oldBin through a local release server so
// the store's previous tag advertises schemas.
func upgradePair(ctx context.Context, t *testing.T, oldBin, newBin, home string) {
	t.Helper()
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(newBin)
	if err != nil {
		t.Fatal(err)
	}
	var hits hitLog
	srv := httptest.NewServer(releaseHandler(t, "v9.0.2", payload, &hits))
	defer srv.Close()
	api := srv.URL + "/repos/gridctl/gridctl/releases?per_page=1"
	base := srv.URL + "/gridctl/gridctl/releases/download"
	stdout, stderr, code := runUpgrade(ctx, t, oldBin, home, api, base, "upgrade", "--yes", "--version", "v9.0.2")
	if code != 0 {
		t.Fatalf("upgrade exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
}

func assertRetainedSchemas(t *testing.T, home, tag string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, ".gridctl", "versions", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var man struct {
		Entries []struct {
			Tag     string          `json:"tag"`
			Schemas json.RawMessage `json:"schemas"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(data, &man); err != nil {
		t.Fatal(err)
	}
	for _, e := range man.Entries {
		if e.Tag != tag {
			continue
		}
		if len(e.Schemas) == 0 || string(e.Schemas) == "null" {
			t.Fatalf("%s schemas = %s", tag, e.Schemas)
		}
		return
	}
	t.Fatalf("tag %s not in manifest", tag)
}

func buildVersioned(ctx context.Context, t *testing.T, root, dir, name, ver string) string {
	t.Helper()
	bin := filepath.Join(dir, name)
	cmd := exec.CommandContext(ctx, "go", "build", "-race", "-ldflags", "-X main.version="+ver, "-o", bin, ".")
	cmd.Dir = filepath.Join(root, "cmd", "gridctl")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build %s: %v\n%s", ver, err, out)
	}
	return bin
}

func runUpgrade(ctx context.Context, t *testing.T, bin, home, api, base string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = upgradeChildEnv(home, api, base)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, stderr.String())
		}
		code = exit.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

func upgradeChildEnv(home, api, base string) []string {
	skip := map[string]bool{
		"HOME": true, "GRIDCTL_HOME": true, "GITHUB_TOKEN": true,
		"GRIDCTL_UPGRADE_RELEASE_API": true, "GRIDCTL_UPGRADE_DOWNLOAD_BASE": true,
	}
	env := make([]string, 0, len(os.Environ())+4)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if skip[key] {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "HOME="+home, "GRIDCTL_HOME="+home)
	if api != "" {
		env = append(env, "GRIDCTL_UPGRADE_RELEASE_API="+api)
	}
	if base != "" {
		env = append(env, "GRIDCTL_UPGRADE_DOWNLOAD_BASE="+base)
	}
	return env
}

func assertVersion(ctx context.Context, t *testing.T, bin, home, want string) {
	t.Helper()
	stdout, stderr, code := runUpgrade(ctx, t, bin, home, "", "", "version")
	if code != 0 {
		t.Fatalf("version exit = %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, want) {
		t.Fatalf("version output = %q, want %s", stdout, want)
	}
	other := "v9.0.1"
	if want == "v9.0.1" {
		other = "v9.0.2"
	}
	if strings.Contains(stdout, other) {
		t.Fatalf("version output = %q, still contains %s", stdout, other)
	}
}

func assertManifest(t *testing.T, home, active, previous string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, ".gridctl", "versions", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var man struct {
		SchemaVersion int    `json:"schema_version"`
		Active        string `json:"active"`
		Previous      string `json:"previous"`
		Entries       []struct {
			Tag string `json:"tag"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(data, &man); err != nil {
		t.Fatal(err)
	}
	if man.SchemaVersion != 1 || man.Active != active || man.Previous != previous {
		t.Fatalf("manifest active=%s previous=%s schema=%d", man.Active, man.Previous, man.SchemaVersion)
	}
	found := map[string]bool{}
	for _, e := range man.Entries {
		found[e.Tag] = true
		path := filepath.Join(home, ".gridctl", "versions", e.Tag, "gridctl")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("retained %s: %v", e.Tag, err)
		}
	}
	if !found[active] || !found[previous] {
		t.Fatalf("entries = %+v", man.Entries)
	}
}

type hitLog struct {
	mu    sync.Mutex
	items []string
}

func (h *hitLog) add(path string) {
	h.mu.Lock()
	h.items = append(h.items, path)
	h.mu.Unlock()
}

func (h *hitLog) paths() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.items...)
}

func (h *hitLog) reset() {
	h.mu.Lock()
	h.items = nil
	h.mu.Unlock()
}

func releaseHandler(t *testing.T, tag string, binary []byte, hits *hitLog) http.Handler {
	t.Helper()
	archive := tarGzBinary(t, binary)
	sum := sha256.Sum256(archive)
	checksum := hex.EncodeToString(sum[:])
	versionNum := strings.TrimPrefix(tag, "v")
	archiveName := fmt.Sprintf("gridctl_%s_%s_%s.tar.gz", versionNum, runtime.GOOS, runtime.GOARCH)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.add(r.URL.Path)
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/gridctl/gridctl/releases"):
			fmt.Fprintf(w, `[{"tag_name":%q}]`, tag)
		case strings.HasSuffix(r.URL.Path, "/"+archiveName):
			_, _ = w.Write(archive)
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			fmt.Fprintf(w, "%s  %s\n", checksum, archiveName)
		default:
			http.NotFound(w, r)
		}
	})
}

func tarGzBinary(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "gridctl", Mode: 0o755, Size: int64(len(payload))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func containsPath(paths []string, fragment string) bool {
	for _, path := range paths {
		if strings.Contains(path, fragment) {
			return true
		}
	}
	return false
}
