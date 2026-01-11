package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setTempHome sets HOME to a temp directory for isolated testing.
// Returns a cleanup function to restore the original HOME.
func setTempHome(t *testing.T) func() {
	t.Helper()
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	return func() {
		os.Setenv("HOME", origHome)
	}
}

func TestBaseDir(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	home := os.Getenv("HOME")
	expected := filepath.Join(home, ".agentlab")
	if got := BaseDir(); got != expected {
		t.Errorf("BaseDir() = %q, want %q", got, expected)
	}
}

func TestStateDir(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	home := os.Getenv("HOME")
	expected := filepath.Join(home, ".agentlab", "state")
	if got := StateDir(); got != expected {
		t.Errorf("StateDir() = %q, want %q", got, expected)
	}
}

func TestLogDir(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	home := os.Getenv("HOME")
	expected := filepath.Join(home, ".agentlab", "logs")
	if got := LogDir(); got != expected {
		t.Errorf("LogDir() = %q, want %q", got, expected)
	}
}

func TestStatePath(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	home := os.Getenv("HOME")
	expected := filepath.Join(home, ".agentlab", "state", "test-topo.json")
	if got := StatePath("test-topo"); got != expected {
		t.Errorf("StatePath(test-topo) = %q, want %q", got, expected)
	}
}

func TestLogPath(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	home := os.Getenv("HOME")
	expected := filepath.Join(home, ".agentlab", "logs", "test-topo.log")
	if got := LogPath("test-topo"); got != expected {
		t.Errorf("LogPath(test-topo) = %q, want %q", got, expected)
	}
}

func TestSave_CreatesFile(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	state := &DaemonState{
		TopologyName: "my-topo",
		TopologyFile: "/path/to/topology.yaml",
		PID:          12345,
		Port:         8080,
		StartedAt:    time.Now(),
	}

	if err := Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify file exists
	path := StatePath("my-topo")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected state file to exist at %s", path)
	}
}

func TestSave_CreatesDirectory(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	state := &DaemonState{
		TopologyName: "my-topo",
		PID:          12345,
	}

	// StateDir doesn't exist yet
	if err := Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify directory was created
	dir := StateDir()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("expected state directory to exist: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected %s to be a directory", dir)
	}
}

func TestLoad_Success(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	startTime := time.Now().Truncate(time.Second)

	// Save a state first
	original := &DaemonState{
		TopologyName: "test-topo",
		TopologyFile: "/path/to/topo.yaml",
		PID:          9999,
		Port:         8080,
		StartedAt:    startTime,
	}
	if err := Save(original); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Load it back
	loaded, err := Load("test-topo")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.TopologyName != original.TopologyName {
		t.Errorf("TopologyName = %q, want %q", loaded.TopologyName, original.TopologyName)
	}
	if loaded.TopologyFile != original.TopologyFile {
		t.Errorf("TopologyFile = %q, want %q", loaded.TopologyFile, original.TopologyFile)
	}
	if loaded.PID != original.PID {
		t.Errorf("PID = %d, want %d", loaded.PID, original.PID)
	}
	if loaded.Port != original.Port {
		t.Errorf("Port = %d, want %d", loaded.Port, original.Port)
	}
	// Compare truncated time to avoid sub-second differences
	if !loaded.StartedAt.Truncate(time.Second).Equal(startTime) {
		t.Errorf("StartedAt = %v, want %v", loaded.StartedAt, startTime)
	}
}

func TestLoad_NotExists(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	_, err := Load("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected IsNotExist error, got %v", err)
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	// Create state directory
	if err := os.MkdirAll(StateDir(), 0755); err != nil {
		t.Fatalf("failed to create state dir: %v", err)
	}

	// Write invalid JSON
	path := StatePath("invalid")
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatalf("failed to write invalid file: %v", err)
	}

	_, err := Load("invalid")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestDelete_Success(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	// Save a state first
	state := &DaemonState{TopologyName: "to-delete", PID: 123}
	if err := Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify file exists
	path := StatePath("to-delete")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected file to exist before delete")
	}

	// Delete it
	if err := Delete("to-delete"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file to be deleted")
	}
}

func TestDelete_NotExists_NoError(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	// Delete should be idempotent - no error for nonexistent file
	if err := Delete("nonexistent"); err != nil {
		t.Errorf("Delete() should not error for nonexistent file, got %v", err)
	}
}

func TestList_Empty(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	states, err := List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(states) != 0 {
		t.Errorf("expected empty list, got %d items", len(states))
	}
}

func TestList_MultipleStates(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	// Save multiple states
	for _, name := range []string{"topo-a", "topo-b", "topo-c"} {
		state := &DaemonState{TopologyName: name, PID: 100}
		if err := Save(state); err != nil {
			t.Fatalf("Save(%s) error = %v", name, err)
		}
	}

	states, err := List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(states) != 3 {
		t.Errorf("expected 3 states, got %d", len(states))
	}
}

func TestList_SkipsInvalidFiles(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	// Create state directory
	if err := os.MkdirAll(StateDir(), 0755); err != nil {
		t.Fatalf("failed to create state dir: %v", err)
	}

	// Save a valid state
	state := &DaemonState{TopologyName: "valid", PID: 100}
	if err := Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Write an invalid JSON file
	invalidPath := filepath.Join(StateDir(), "invalid.json")
	if err := os.WriteFile(invalidPath, []byte("not json"), 0644); err != nil {
		t.Fatalf("failed to write invalid file: %v", err)
	}

	// Write a non-JSON file
	nonJSONPath := filepath.Join(StateDir(), "readme.txt")
	if err := os.WriteFile(nonJSONPath, []byte("readme"), 0644); err != nil {
		t.Fatalf("failed to write non-JSON file: %v", err)
	}

	states, err := List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	// Should only return the valid state
	if len(states) != 1 {
		t.Errorf("expected 1 valid state, got %d", len(states))
	}
}

func TestIsRunning_NilState(t *testing.T) {
	if IsRunning(nil) {
		t.Error("expected IsRunning(nil) to be false")
	}
}

func TestIsRunning_ZeroPID(t *testing.T) {
	state := &DaemonState{TopologyName: "test", PID: 0}
	if IsRunning(state) {
		t.Error("expected IsRunning with PID=0 to be false")
	}
}

func TestIsRunning_CurrentProcess(t *testing.T) {
	// Use current process - this should be running
	state := &DaemonState{TopologyName: "test", PID: os.Getpid()}
	if !IsRunning(state) {
		t.Error("expected IsRunning for current process to be true")
	}
}

func TestIsRunning_InvalidPID(t *testing.T) {
	// Use a very high PID that's unlikely to exist
	state := &DaemonState{TopologyName: "test", PID: 999999999}
	if IsRunning(state) {
		t.Error("expected IsRunning for invalid PID to be false")
	}
}

func TestKillDaemon_NilState(t *testing.T) {
	// Should not error
	if err := KillDaemon(nil); err != nil {
		t.Errorf("KillDaemon(nil) error = %v", err)
	}
}

func TestKillDaemon_ZeroPID(t *testing.T) {
	state := &DaemonState{TopologyName: "test", PID: 0}
	// Should not error
	if err := KillDaemon(state); err != nil {
		t.Errorf("KillDaemon with PID=0 error = %v", err)
	}
}

func TestEnsureLogDir(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	if err := EnsureLogDir(); err != nil {
		t.Fatalf("EnsureLogDir() error = %v", err)
	}

	// Verify directory exists
	dir := LogDir()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("expected log directory to exist: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected %s to be a directory", dir)
	}
}

func TestEnsureLogDir_Idempotent(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	// Call twice - should not error
	if err := EnsureLogDir(); err != nil {
		t.Fatalf("first EnsureLogDir() error = %v", err)
	}
	if err := EnsureLogDir(); err != nil {
		t.Fatalf("second EnsureLogDir() error = %v", err)
	}
}

func TestMigrateFromAgent0_NoOldDir(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	// Should not error when old dir doesn't exist
	if err := MigrateFromAgent0(); err != nil {
		t.Errorf("MigrateFromAgent0() error = %v", err)
	}
}

func TestMigrateFromAgent0_NewDirExists(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	home := os.Getenv("HOME")

	// Create both old and new dirs
	oldDir := filepath.Join(home, ".agent0")
	newDir := filepath.Join(home, ".agentlab")
	if err := os.MkdirAll(oldDir, 0755); err != nil {
		t.Fatalf("failed to create old dir: %v", err)
	}
	if err := os.MkdirAll(newDir, 0755); err != nil {
		t.Fatalf("failed to create new dir: %v", err)
	}

	// Should not migrate when new dir already exists
	if err := MigrateFromAgent0(); err != nil {
		t.Errorf("MigrateFromAgent0() error = %v", err)
	}

	// Old dir should still exist (not renamed)
	if _, err := os.Stat(oldDir); os.IsNotExist(err) {
		t.Error("expected old dir to still exist")
	}
}

func TestMigrateFromAgent0_Success(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	home := os.Getenv("HOME")

	// Create old dir with a file
	oldDir := filepath.Join(home, ".agent0")
	if err := os.MkdirAll(filepath.Join(oldDir, "state"), 0755); err != nil {
		t.Fatalf("failed to create old dir: %v", err)
	}
	testFile := filepath.Join(oldDir, "state", "test.json")
	if err := os.WriteFile(testFile, []byte("{}"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Migrate
	if err := MigrateFromAgent0(); err != nil {
		t.Fatalf("MigrateFromAgent0() error = %v", err)
	}

	// New dir should exist with the file
	newDir := filepath.Join(home, ".agentlab")
	newFile := filepath.Join(newDir, "state", "test.json")
	if _, err := os.Stat(newFile); os.IsNotExist(err) {
		t.Error("expected migrated file to exist in new location")
	}

	// Old dir should be gone
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Error("expected old dir to be removed after migration")
	}
}

func TestLockPath(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	home := os.Getenv("HOME")
	expected := filepath.Join(home, ".agentlab", "state", "test-topo.lock")
	if got := LockPath("test-topo"); got != expected {
		t.Errorf("LockPath(test-topo) = %q, want %q", got, expected)
	}
}

func TestWithLock_ExecutesCallback(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	called := false
	err := WithLock("test-topo", 1*time.Second, func() error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithLock() error = %v", err)
	}
	if !called {
		t.Error("expected callback to be called")
	}
}

func TestWithLock_ReturnsCallbackError(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	expectedErr := os.ErrNotExist
	err := WithLock("test-topo", 1*time.Second, func() error {
		return expectedErr
	})
	if err != expectedErr {
		t.Errorf("WithLock() error = %v, want %v", err, expectedErr)
	}
}

func TestWithLock_CreatesDirectory(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	// State directory doesn't exist yet
	err := WithLock("test-topo", 1*time.Second, func() error {
		return nil
	})
	if err != nil {
		t.Fatalf("WithLock() error = %v", err)
	}

	// Verify directory was created
	dir := StateDir()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("expected state directory to exist: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected %s to be a directory", dir)
	}
}

func TestWithLock_ExclusiveAccess(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	// Acquire lock and hold it while checking another can't acquire
	lockAcquired := make(chan struct{})
	done := make(chan struct{})

	go func() {
		err := WithLock("test-topo", 5*time.Second, func() error {
			close(lockAcquired) // Signal that lock is held
			<-done              // Wait for test to finish
			return nil
		})
		if err != nil {
			t.Errorf("first WithLock() error = %v", err)
		}
	}()

	// Wait for first goroutine to acquire lock
	<-lockAcquired

	// Try to acquire same lock with short timeout - should fail
	err := WithLock("test-topo", 100*time.Millisecond, func() error {
		t.Error("second callback should not have been called")
		return nil
	})
	if err == nil {
		t.Error("expected timeout error when lock is held")
	}

	// Allow first goroutine to release lock
	close(done)
}

func TestCheckAndClean_NoStateFile(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	cleaned, err := CheckAndClean("nonexistent")
	if err != nil {
		t.Fatalf("CheckAndClean() error = %v", err)
	}
	if cleaned {
		t.Error("expected cleaned=false when no state file exists")
	}
}

func TestCheckAndClean_RunningProcess(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	// Save state with current process PID (which is running)
	state := &DaemonState{
		TopologyName: "test-topo",
		PID:          os.Getpid(),
	}
	if err := Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	cleaned, err := CheckAndClean("test-topo")
	if err != nil {
		t.Fatalf("CheckAndClean() error = %v", err)
	}
	if cleaned {
		t.Error("expected cleaned=false when process is running")
	}

	// State file should still exist
	if _, err := Load("test-topo"); err != nil {
		t.Error("expected state file to still exist")
	}
}

func TestCheckAndClean_DeadProcess(t *testing.T) {
	cleanup := setTempHome(t)
	defer cleanup()

	// Save state with a PID that doesn't exist
	state := &DaemonState{
		TopologyName: "test-topo",
		PID:          999999999, // Very high PID unlikely to exist
	}
	if err := Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	cleaned, err := CheckAndClean("test-topo")
	if err != nil {
		t.Fatalf("CheckAndClean() error = %v", err)
	}
	if !cleaned {
		t.Error("expected cleaned=true when process is dead")
	}

	// State file should be deleted
	if _, err := Load("test-topo"); !os.IsNotExist(err) {
		t.Error("expected state file to be deleted")
	}
}
