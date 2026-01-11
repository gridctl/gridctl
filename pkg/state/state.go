package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// DaemonState represents the state of a running daemon.
type DaemonState struct {
	TopologyName string    `json:"topology_name"`
	TopologyFile string    `json:"topology_file"`
	PID          int       `json:"pid"`
	Port         int       `json:"port"`
	StartedAt    time.Time `json:"started_at"`
}

// BaseDir returns the base agentlab directory (~/.agentlab/).
func BaseDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agentlab")
}

// StateDir returns the directory for state files (~/.agentlab/state/).
func StateDir() string {
	return filepath.Join(BaseDir(), "state")
}

// LogDir returns the directory for log files (~/.agentlab/logs/).
func LogDir() string {
	return filepath.Join(BaseDir(), "logs")
}

// MigrateFromAgent0 moves data from ~/.agent0 to ~/.agentlab if needed.
// This ensures backwards compatibility with existing installations.
func MigrateFromAgent0() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	oldDir := filepath.Join(home, ".agent0")
	newDir := filepath.Join(home, ".agentlab")

	// Skip if old dir doesn't exist
	if _, err := os.Stat(oldDir); os.IsNotExist(err) {
		return nil
	}

	// Skip if new dir already exists (don't overwrite)
	if _, err := os.Stat(newDir); err == nil {
		return nil
	}

	// Rename old to new
	fmt.Printf("Migrating data from %s to %s...\n", oldDir, newDir)
	return os.Rename(oldDir, newDir)
}

// StatePath returns the path to a state file for a topology.
func StatePath(name string) string {
	return filepath.Join(StateDir(), name+".json")
}

// LogPath returns the path to a log file for a topology.
func LogPath(name string) string {
	return filepath.Join(LogDir(), name+".log")
}

// LockPath returns the path to a lock file for a topology.
func LockPath(name string) string {
	return filepath.Join(StateDir(), name+".lock")
}

// Load reads a daemon state file.
func Load(name string) (*DaemonState, error) {
	data, err := os.ReadFile(StatePath(name))
	if err != nil {
		return nil, err
	}

	var state DaemonState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}

	return &state, nil
}

// Save writes a daemon state file.
func Save(state *DaemonState) error {
	// Ensure directory exists
	if err := os.MkdirAll(StateDir(), 0755); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	if err := os.WriteFile(StatePath(state.TopologyName), data, 0644); err != nil {
		return fmt.Errorf("writing state file: %w", err)
	}

	return nil
}

// Delete removes a state file.
func Delete(name string) error {
	path := StatePath(name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// List returns all daemon states.
func List() ([]DaemonState, error) {
	dir := StateDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var states []DaemonState
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		name := entry.Name()[:len(entry.Name())-5] // Remove .json
		state, err := Load(name)
		if err != nil {
			continue // Skip invalid state files
		}
		states = append(states, *state)
	}

	return states, nil
}

// IsRunning checks if the daemon process is still running.
func IsRunning(state *DaemonState) bool {
	if state == nil {
		return false
	}
	return VerifyPID(state.PID)
}

// VerifyPID checks if a process with the given PID is running.
func VerifyPID(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks for existence without killing
	return process.Signal(syscall.Signal(0)) == nil
}

// CheckAndClean checks if a state file exists and if the process is running.
// If the process is dead, it removes the state file and returns true (cleaned).
// If the process is running, it returns false (not cleaned).
// If no state file exists, it returns false.
func CheckAndClean(name string) (bool, error) {
	st, err := Load(name)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		// If we can't load the state file (corrupt), we should probably clean it
		// Try to delete it so we can start fresh
		if delErr := Delete(name); delErr != nil {
			return false, fmt.Errorf("state file corrupt and failed to delete: %w", delErr)
		}
		return true, nil
	}

	if VerifyPID(st.PID) {
		return false, nil
	}

	// Process is dead, clean up
	if err := Delete(name); err != nil {
		return false, err
	}
	return true, nil
}

// KillDaemon sends SIGTERM to the daemon process, waits up to 5 seconds for
// graceful shutdown, then sends SIGKILL if the process is still running.
func KillDaemon(state *DaemonState) error {
	if state == nil || state.PID == 0 {
		return nil
	}

	process, err := os.FindProcess(state.PID)
	if err != nil {
		return fmt.Errorf("finding process %d: %w", state.PID, err)
	}

	// Check if already dead
	if !VerifyPID(state.PID) {
		return nil
	}

	// Send SIGTERM for graceful shutdown
	if err := process.Signal(syscall.SIGTERM); err != nil {
		if err == os.ErrProcessDone {
			return nil
		}
		return fmt.Errorf("sending SIGTERM to %d: %w", state.PID, err)
	}

	// Wait up to 5 seconds for graceful shutdown
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !VerifyPID(state.PID) {
			return nil // Process exited gracefully
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Process still running, send SIGKILL
	if err := process.Signal(syscall.SIGKILL); err != nil {
		if err == os.ErrProcessDone {
			return nil
		}
		return fmt.Errorf("sending SIGKILL to %d: %w", state.PID, err)
	}

	return nil
}

// EnsureLogDir creates the log directory if it doesn't exist.
func EnsureLogDir() error {
	return os.MkdirAll(LogDir(), 0755)
}

// WithLock executes fn while holding an exclusive lock on the topology state.
// Returns error if lock cannot be acquired within timeout.
func WithLock(name string, timeout time.Duration, fn func() error) error {
	// Ensure directory exists
	if err := os.MkdirAll(StateDir(), 0755); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}

	lockFile, err := os.OpenFile(LockPath(name), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("opening lock file: %w", err)
	}
	defer lockFile.Close()

	// Try to acquire lock with timeout
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout acquiring state lock for %s (another operation may be in progress)", name)
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Lock acquired - ensure we unlock before closing file
	defer func() {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	}()

	return fn()
}
