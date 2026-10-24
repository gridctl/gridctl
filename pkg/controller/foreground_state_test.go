package controller

import (
	"os"
	"testing"

	"github.com/gridctl/gridctl/pkg/state"
)

func TestSaveForegroundState_RecordsBinaryVersion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(state.HomeEnv, dir)

	sc := New(Config{Port: 8191})
	sc.SetVersion("v9.9.1")
	cleanup, err := sc.saveForegroundState("lab", "/tmp/lab.yaml", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)

	st, err := state.Load("lab")
	if err != nil {
		t.Fatal(err)
	}
	if st.BinaryVersion != "v9.9.1" {
		t.Fatalf("BinaryVersion = %q", st.BinaryVersion)
	}
	if st.StackFile != "/tmp/lab.yaml" || st.Port != 8191 {
		t.Fatalf("state = %+v", st)
	}
}

func TestLoadDaemonState_AbsentBinaryVersion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(state.HomeEnv, dir)
	if err := state.Save(&state.DaemonState{StackName: "old", PID: 1}); err != nil {
		t.Fatal(err)
	}
	// Rewrite without binary_version, the shape an older gridctl wrote.
	path, err := state.StatePath("old")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{\"schema_version\":2,\"stack_name\":\"old\",\"pid\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := state.Load("old")
	if err != nil {
		t.Fatal(err)
	}
	if st.BinaryVersion != "" {
		t.Fatalf("BinaryVersion = %q, want empty", st.BinaryVersion)
	}
}
