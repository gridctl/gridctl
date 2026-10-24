package versions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("VERSIONS_LOCK_HELPER") == "1" {
		runLockHelper()
		return
	}
	os.Exit(m.Run())
}

func runLockHelper() {
	dir := os.Getenv("VERSIONS_LOCK_DIR")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := Open(ctx, dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := os.Stdout.WriteString("held\n"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = os.Stdout.Sync()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
			_ = store.Close()
			os.Exit(0)
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = store.Close()
	fmt.Fprintln(os.Stderr, "helper timed out waiting for release")
	os.Exit(2)
}

func TestStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()
	src := writeFile(t, filepath.Join(t.TempDir(), "gridctl"), []byte("binary-v1"))
	schemas := &Schemas{DaemonState: 2, ProjectLock: 1, ImportLock: 3, ToolPins: "2", SkillPins: "1"}

	s := openStore(t, ctx, dir)
	when := time.Date(2026, 10, 23, 14, 2, 12, 0, time.UTC)
	s.now = func() time.Time { return when }
	prev := writeFile(t, filepath.Join(t.TempDir(), "prev"), []byte("binary-v0"))
	if err := s.Retain(ctx, "v1.0.0", prev, OriginReplaced, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Retain(ctx, "v1.2.3", src, OriginDownloaded, schemas); err != nil {
		t.Fatal(err)
	}
	if err := s.SetActive("v1.2.3", "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(dir, "v1.2.3", "gridctl"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("binary mode = %o, want 755", info.Mode().Perm())
	}
	manInfo, err := os.Stat(filepath.Join(dir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	if manInfo.Mode().Perm() != 0o644 {
		t.Errorf("manifest mode = %o, want 644", manInfo.Mode().Perm())
	}
	raw, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"schemas": {`)) {
		t.Fatalf("manifest missing schemas object:\n%s", raw)
	}

	reopened := openStore(t, ctx, dir)
	defer reopened.Close()
	entry, path, ok := reopened.Lookup("v1.2.3")
	if !ok {
		t.Fatal("lookup missed retained tag")
	}
	if entry.Origin != OriginDownloaded || entry.Size != int64(len("binary-v1")) || !entry.RetainedAt.Equal(when) {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.Schemas == nil || entry.Schemas.ToolPins != "2" || entry.Schemas.SkillPins != "1" || entry.Schemas.DaemonState != 2 {
		t.Fatalf("schemas = %+v", entry.Schemas)
	}
	if path != filepath.Join(dir, "v1.2.3", "gridctl") {
		t.Fatalf("path = %s", path)
	}
	if err := reopened.Verify(ctx, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if reopened.Active() != "v1.2.3" || reopened.Previous() != "v1.0.0" {
		t.Fatalf("active=%s previous=%s", reopened.Active(), reopened.Previous())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "binary-v1" {
		t.Fatalf("binary = %q", got)
	}
}

func TestStore_SchemasNull(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()
	src := writeFile(t, filepath.Join(t.TempDir(), "gridctl"), []byte("bin"))
	s := openStore(t, ctx, dir)
	defer s.Close()
	if err := s.Retain(ctx, "v1.0.0", src, OriginReplaced, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"schemas": null`)) {
		t.Fatalf("nil schemas must serialize as null:\n%s", raw)
	}
	var man Manifest
	if err := json.Unmarshal(raw, &man); err != nil {
		t.Fatal(err)
	}
	if len(man.Entries) != 1 || man.Entries[0].Schemas != nil {
		t.Fatalf("decoded schemas = %#v", man.Entries[0].Schemas)
	}
}

func TestStore_NewerSchema(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("{\n  \"schema_version\": 9,\n  \"active\": \"v9.0.0\",\n  \"previous\": \"\",\n  \"entries\": []\n}\n")
	if err := os.WriteFile(filepath.Join(dir, manifestName), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Open(t.Context(), dir)
	if !errors.Is(err, ErrNewerManifest) {
		t.Fatalf("Open err = %v, want ErrNewerManifest", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("newer manifest was rewritten:\n%s", got)
	}
}

func TestStore_SameSHANoopAndReplace(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()
	src := writeFile(t, filepath.Join(t.TempDir(), "gridctl"), []byte("same"))
	s := openStore(t, ctx, dir)
	defer s.Close()
	first := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	s.now = func() time.Time { return first }
	if err := s.Retain(ctx, "v1.0.0", src, OriginDownloaded, nil); err != nil {
		t.Fatal(err)
	}
	later := first.Add(time.Hour)
	s.now = func() time.Time { return later }
	if err := s.Retain(ctx, "v1.0.0", src, OriginReplaced, &Schemas{DaemonState: 2}); err != nil {
		t.Fatal(err)
	}
	entry, _, ok := s.Lookup("v1.0.0")
	if !ok {
		t.Fatal("missing entry")
	}
	if !entry.RetainedAt.Equal(first) || entry.Origin != OriginDownloaded || entry.Schemas != nil {
		t.Fatalf("same SHA must be a no-op, got %+v", entry)
	}

	other := writeFile(t, filepath.Join(t.TempDir(), "gridctl"), []byte("other"))
	if err := s.Retain(ctx, "v1.0.0", other, OriginReplaced, nil); err != nil {
		t.Fatal(err)
	}
	entry, path, _ := s.Lookup("v1.0.0")
	if !entry.RetainedAt.Equal(later) || entry.Origin != OriginReplaced {
		t.Fatalf("replaced entry = %+v", entry)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "other" {
		t.Fatalf("binary = %q", got)
	}
}

func TestStore_VerifyMismatch(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()
	src := writeFile(t, filepath.Join(t.TempDir(), "gridctl"), []byte("good"))
	s := openStore(t, ctx, dir)
	defer s.Close()
	if err := s.Retain(ctx, "v1.0.0", src, OriginDownloaded, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "v1.0.0", "gridctl"), []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := s.Verify(ctx, "v1.0.0")
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("Verify err = %v", err)
	}
	if !strings.Contains(err.Error(), "expected:") || !strings.Contains(err.Error(), "actual:") {
		t.Fatalf("mismatch error = %q", err)
	}
}

func TestStore_PruneOrderingAndFloor(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()
	s := openStore(t, ctx, dir)
	defer s.Close()
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i, tag := range []string{"v1.0.0", "v1.1.0", "v1.2.0", "v1.3.0"} {
		when := base.Add(time.Duration(i) * time.Hour)
		s.now = func() time.Time { return when }
		src := writeFile(t, filepath.Join(t.TempDir(), tag), []byte(tag))
		if err := s.Retain(ctx, tag, src, OriginDownloaded, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetActive("v1.3.0", "v1.2.0"); err != nil {
		t.Fatal(err)
	}
	removed, err := s.Prune(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(removed, ",") != "v1.0.0,v1.1.0" {
		t.Fatalf("removed = %v, want oldest first and floor kept", removed)
	}
	got := tags(s.Entries())
	if strings.Join(got, ",") != "v1.3.0,v1.2.0" {
		t.Fatalf("entries = %v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "v1.0.0")); !os.IsNotExist(err) {
		t.Fatalf("pruned tag directory still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "v1.2.0", "gridctl")); err != nil {
		t.Fatal(err)
	}
}

func TestStore_MissingFileReconciliationAndOrphans(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()
	s := openStore(t, ctx, dir)
	src := writeFile(t, filepath.Join(t.TempDir(), "gridctl"), []byte("keep"))
	if err := s.Retain(ctx, "v1.0.0", src, OriginDownloaded, nil); err != nil {
		t.Fatal(err)
	}
	other := writeFile(t, filepath.Join(t.TempDir(), "other"), []byte("drop"))
	if err := s.Retain(ctx, "v0.9.0", other, OriginReplaced, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SetActive("v1.0.0", "v0.9.0"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "v0.9.0", "gridctl")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "v0.1.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.SetActive("v1.0.0", "v0.9.0"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.Lookup("v0.9.0"); ok {
		t.Fatal("missing binary must be dropped from the manifest")
	}
	if s.Previous() != "" {
		t.Fatalf("previous = %q, want cleared after the file was dropped", s.Previous())
	}
	orphans := s.Orphans()
	if strings.Join(orphans, ",") != "v0.1.0,v0.9.0" {
		t.Fatalf("orphans = %v", orphans)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	again := openStore(t, ctx, dir)
	defer again.Close()
	if _, err := again.Prune(ctx, 1); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"v0.1.0", "v0.9.0"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("orphan %s was deleted: %v", name, err)
		}
	}
}

func TestStore_RejectsUnsafeManifestTags(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{
			name: "entry",
			raw:  `{"schema_version":1,"active":"","previous":"","entries":[{"tag":"../../bin/x","sha256":"aa","size":1,"origin":"downloaded","schemas":null}]}`,
		},
		{
			name: "previous",
			raw:  `{"schema_version":1,"active":"v1.0.0","previous":"../../bin/x","entries":[{"tag":"v1.0.0","sha256":"aa","size":1,"origin":"downloaded","schemas":null}]}`,
		},
		{
			name: "active",
			raw:  `{"schema_version":1,"active":"v1/evil","previous":"","entries":[]}`,
		},
		{
			name: "backslash",
			raw:  `{"schema_version":1,"active":"","previous":"v1\\evil","entries":[]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, manifestName)
			if err := os.WriteFile(path, []byte(tc.raw), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Open(t.Context(), dir)
			if !errors.Is(err, ErrInvalidTag) {
				t.Fatalf("Open err = %v, want ErrInvalidTag", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.raw {
				t.Fatalf("manifest rewritten:\n%s", got)
			}
		})
	}
}

func TestStore_RejectsUnsafeTagAndSymlink(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()
	s := openStore(t, ctx, dir)
	defer s.Close()
	src := writeFile(t, filepath.Join(t.TempDir(), "gridctl"), []byte("bin"))
	for _, tag := range []string{"", ".", "..", ".hidden", "v1/evil", `v1\evil`, "v1..0", "../x"} {
		if err := s.Retain(ctx, tag, src, OriginDownloaded, nil); !errors.Is(err, ErrInvalidTag) {
			t.Errorf("Retain(%q) err = %v, want ErrInvalidTag", tag, err)
		}
	}
	outside := writeFile(t, filepath.Join(t.TempDir(), "outside"), []byte("secret"))
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := s.Retain(ctx, "v1.0.0", link, OriginDownloaded, nil); !errors.Is(err, ErrSymlinkSource) {
		t.Fatalf("unallowed symlink err = %v", err)
	}
	allowedDir := t.TempDir()
	inside := writeFile(t, filepath.Join(allowedDir, "gridctl"), []byte("inside"))
	allowedLink := filepath.Join(allowedDir, "link")
	if err := os.Symlink(inside, allowedLink); err != nil {
		t.Fatal(err)
	}
	s.AllowSources(allowedDir)
	if err := s.Retain(ctx, "v1.0.0", allowedLink, OriginDownloaded, nil); err != nil {
		t.Fatal(err)
	}
}

func TestStore_CancelledRetain(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, filepath.Join(t.TempDir(), "gridctl"), []byte("bin"))
	s := openStore(t, t.Context(), dir)
	defer s.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Retain(ctx, "v1.0.0", src, OriginDownloaded, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Retain err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, manifestName)); !os.IsNotExist(err) {
		t.Fatal("cancelled retain must not write the manifest")
	}
}

func TestStore_LockContention(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestStore_LockContention") //nolint:gosec // G702: re-executes this test binary to hold the versions lock
	cmd.Env = append(os.Environ(), "VERSIONS_LOCK_HELPER=1", "VERSIONS_LOCK_DIR="+dir)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	buf := make([]byte, 8)
	if _, err := stdout.Read(buf); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(buf), "held") {
		t.Fatalf("helper stdout = %q", buf)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	if _, err := Open(ctx, dir); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second Open err = %v, want deadline while the other process holds the lock", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "release"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("helper did not exit")
	}

	opened, err := Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	opened.Close()
}

func TestStore_EntriesNewestFirst(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()
	s := openStore(t, ctx, dir)
	defer s.Close()
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	for i, tag := range []string{"v1.0.0", "v1.1.0"} {
		when := base.Add(time.Duration(i) * time.Minute)
		s.now = func() time.Time { return when }
		src := writeFile(t, filepath.Join(t.TempDir(), tag), []byte(tag))
		if err := s.Retain(ctx, tag, src, OriginReplaced, nil); err != nil {
			t.Fatal(err)
		}
	}
	got := tags(s.Entries())
	if strings.Join(got, ",") != "v1.1.0,v1.0.0" {
		t.Fatalf("entries = %v", got)
	}
}

func openStore(t *testing.T, ctx context.Context, dir string) *Store {
	t.Helper()
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func writeFile(t *testing.T, path string, data []byte) string {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func tags(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Tag
	}
	return out
}
