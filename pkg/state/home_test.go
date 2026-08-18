package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHome_EnvOverrideWins(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(HomeEnv, dir)

	got, err := Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if got != filepath.Clean(dir) {
		t.Errorf("Home() = %q, want %q", got, dir)
	}
	if !HomeOverridden() {
		t.Error("HomeOverridden() = false, want true")
	}
}

func TestHome_RelativeOverrideRejected(t *testing.T) {
	t.Setenv(HomeEnv, "relative/path")

	_, err := Home()
	if err == nil {
		t.Fatal("Home() accepted a relative GRIDCTL_HOME; a relative root could aim RemoveAll at the working directory")
	}
	if !strings.Contains(err.Error(), HomeEnv) {
		t.Errorf("error should name %s, got: %v", HomeEnv, err)
	}
}

func TestHome_FallsBackToOSHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(HomeEnv, "")

	got, err := Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if got != home {
		t.Errorf("Home() = %q, want %q", got, home)
	}
	if HomeOverridden() {
		t.Error("HomeOverridden() = true, want false")
	}
}

func TestHome_NoHomeAtAllErrors(t *testing.T) {
	t.Setenv(HomeEnv, "")
	t.Setenv("HOME", "")

	if _, err := Home(); err == nil {
		t.Fatal("Home() should error with no GRIDCTL_HOME and no HOME, never fall back to a relative path")
	}
	if _, err := BaseDir(); err == nil {
		t.Fatal("BaseDir() should propagate the resolution error, never return a relative .gridctl")
	}
}

func TestBaseDir_UnderOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(HomeEnv, dir)

	got, err := BaseDir()
	if err != nil {
		t.Fatalf("BaseDir: %v", err)
	}
	want := filepath.Join(dir, ".gridctl")
	if got != want {
		t.Errorf("BaseDir() = %q, want %q", got, want)
	}
}

func TestDaemonState_RecordsHomeAndVersion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(HomeEnv, dir)

	if err := Save(&DaemonState{StackName: "vtest", PID: 999999}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	st, err := Load("vtest")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if st.SchemaVersion != daemonStateSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", st.SchemaVersion, daemonStateSchemaVersion)
	}
	if st.Home != filepath.Clean(dir) {
		t.Errorf("Home = %q, want %q", st.Home, dir)
	}
}

func TestDaemonState_RefusesNewerSchema(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(HomeEnv, dir)

	if err := Save(&DaemonState{StackName: "newer", PID: 999999}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Rewrite with a future schema version.
	st, _ := Load("newer")
	st.SchemaVersion = daemonStateSchemaVersion + 1
	// Save() stamps the current version, so write the future file by hand.
	path, err := StatePath("newer")
	if err != nil {
		t.Fatalf("StatePath: %v", err)
	}
	writeStateFileRaw(t, path, st)

	if _, err := Load("newer"); err == nil {
		t.Fatal("Load() accepted a newer schema version; Article XVII requires refusing it")
	}
}

// writeStateFileRaw marshals st to path without Save's version stamping.
func writeStateFileRaw(t *testing.T, path string, st *DaemonState) {
	t.Helper()
	// #nosec G117 -- test helper writing a synthetic state file; the real
	// write path's justification lives in Save.
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
}
