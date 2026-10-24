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
	"strings"
	"sync"
	"testing"
	"time"
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
	if !strings.Contains(stderr, "compatibility unknown: v9.0.1") {
		t.Fatalf("stderr = %s", stderr)
	}
	assertVersion(ctx, t, oldBin, home, "v9.0.1")
	assertManifest(t, home, "v9.0.1", "v9.0.2")
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
