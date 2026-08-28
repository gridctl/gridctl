package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const daemonShutdownGracePeriod = 5 * time.Second

// daemonStateSchemaVersion is the current schema of the per-stack daemon
// state file. Version 1 is the pre-versioned file (schema_version absent);
// version 2 adds the schema_version and home fields. Readers refuse a newer
// version rather than guess (Article XVII).
const daemonStateSchemaVersion = 2

// ErrNewerSchema marks a state file written by a newer gridctl. Callers
// must refuse to act on it, and above all must not "clean it up": the
// file belongs to a daemon this binary cannot represent.
var ErrNewerSchema = errors.New("state file written by a newer gridctl")

// DaemonState represents the state of a running daemon.
type DaemonState struct {
	// SchemaVersion identifies the state-file schema. Absent (0) in
	// files written before versioning; treated as version 1 on read.
	SchemaVersion int       `json:"schema_version,omitempty"`
	StackName     string    `json:"stack_name"`
	StackFile     string    `json:"stack_file"`
	PID           int       `json:"pid"`
	Port          int       `json:"port"`
	StartedAt     time.Time `json:"started_at"`

	// Home is the resolved home directory the daemon was started under
	// (see Home()). Subcommands compare it against their own resolved
	// home so a GRIDCTL_HOME mismatch surfaces as a named warning
	// instead of a confusing empty state.
	Home string `json:"home,omitempty"`

	// AuthToken and AuthHeader carry the gateway's inbound credentials so
	// local subcommands can authenticate against the API they already know
	// the port of. Empty when gateway.auth is not configured.
	//
	// The daemon records the token already resolved, because the config
	// loader expands ${VAR} references before the value reaches it. The
	// alternative — each subcommand loading the stack and expanding it —
	// would turn a `gridctl status` into a vault passphrase prompt.
	//
	// This does place a resolved secret on disk. The state file is written
	// 0600 (see SaveDaemonState), matching the vault's own plaintext
	// secrets.json and the machine key in pkg/mcpauth, so it is consistent
	// with the existing posture rather than a new exposure.
	AuthToken  string `json:"auth_token,omitempty"`
	AuthHeader string `json:"auth_header,omitempty"`
	AuthType   string `json:"auth_type,omitempty"`
}

// BaseDir returns the base gridctl directory (<home>/.gridctl). It errors
// rather than falling back to a relative path when the home cannot be
// resolved: every caller below joins onto this value, and a destructive
// operation aimed at a relative ".gridctl" would target the working
// directory.
func BaseDir() (string, error) {
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gridctl"), nil
}

// StateDir returns the directory for state files (<home>/.gridctl/state).
func StateDir() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "state"), nil
}

// LogDir returns the directory for log files (<home>/.gridctl/logs).
func LogDir() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "logs"), nil
}

// VaultDir returns the directory for vault storage (<home>/.gridctl/vault).
func VaultDir() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "vault"), nil
}

// PinsDir returns the directory for schema pin files (<home>/.gridctl/pins).
func PinsDir() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "pins"), nil
}

// StacksDir returns the directory for saved stack files (<home>/.gridctl/stacks).
func StacksDir() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "stacks"), nil
}

// PinsPath returns the path to the pin file for a stack (<home>/.gridctl/pins/{name}.json).
func PinsPath(name string) (string, error) {
	dir, err := PinsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".json"), nil
}

// SkillPinsPath returns the path to the skill pin file for a stack
// (<home>/.gridctl/pins/skills/{name}.json). A subdirectory, not a filename
// suffix, keeps the namespace disjoint from tool pins: any suffix scheme
// inside PinsDir would collide with a stack literally named with that
// suffix (PinsPath("x.skills") == a suffix-based SkillPinsPath("x")).
// Skill pins track registry documents, not live tool sets, and the two
// stores version independently.
func SkillPinsPath(name string) (string, error) {
	dir, err := PinsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "skills", name+".json"), nil
}

// StatePath returns the path to a state file for a stack.
func StatePath(name string) (string, error) {
	dir, err := StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".json"), nil
}

// LogPath returns the path to a log file for a stack.
func LogPath(name string) (string, error) {
	dir, err := LogDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".log"), nil
}

// LockPath returns the path to a lock file for a stack.
func LockPath(name string) (string, error) {
	dir, err := StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".lock"), nil
}

// Load reads a daemon state file.
func Load(name string) (*DaemonState, error) {
	path, err := StatePath(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var state DaemonState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}

	// Article XVII: refuse a newer schema instead of guessing at it.
	// Absent (0) is the pre-versioned file, read as version 1.
	if state.SchemaVersion > daemonStateSchemaVersion {
		return nil, fmt.Errorf("%w: %s has schema version %d, newer than this gridctl understands (%d); upgrade gridctl",
			ErrNewerSchema, path, state.SchemaVersion, daemonStateSchemaVersion)
	}

	return &state, nil
}

// Save writes a daemon state file, stamping the current schema version and
// the resolved home the daemon runs under.
func Save(state *DaemonState) error {
	dir, err := StateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}

	state.SchemaVersion = daemonStateSchemaVersion
	if state.Home == "" {
		if home, err := Home(); err == nil {
			state.Home = home
		}
	}

	// #nosec G117 -- AuthToken is intentionally persisted: local subcommands
	// need the gateway credential to call the API, and the alternative (each
	// command loading the stack and expanding ${VAR}) would prompt for the
	// vault passphrase on every invocation. The file is written 0600 below.
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	path, err := StatePath(state.StackName)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing state file: %w", err)
	}

	return nil
}

// Delete removes a state file.
func Delete(name string) error {
	path, err := StatePath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// List returns all daemon states.
func List() ([]DaemonState, error) {
	dir, err := StateDir()
	if err != nil {
		return nil, err
	}
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
	return processExists(process)
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
		// A newer-schema file is NOT corruption: it belongs to a daemon a
		// newer binary is running. Deleting it would orphan that daemon.
		if errors.Is(err, ErrNewerSchema) {
			return false, err
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
	if err := terminateProcess(process); err != nil {
		if err == os.ErrProcessDone {
			return nil
		}
		return fmt.Errorf("sending SIGTERM to %d: %w", state.PID, err)
	}

	// Wait up to 5 seconds for graceful shutdown
	deadline := time.Now().Add(daemonShutdownGracePeriod)
	for time.Now().Before(deadline) {
		if !VerifyPID(state.PID) {
			return nil // Process exited gracefully
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Process still running, send SIGKILL
	if err := killProcess(process); err != nil {
		if err == os.ErrProcessDone {
			return nil
		}
		return fmt.Errorf("sending SIGKILL to %d: %w", state.PID, err)
	}

	return nil
}

// EnsureLogDir creates the log directory if it doesn't exist.
func EnsureLogDir() error {
	dir, err := LogDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0755)
}

// TelemetryDir returns the root directory for opt-in telemetry persistence
// (<home>/.gridctl/telemetry). Subtree layout: <stack>/<server>/{logs,metrics,traces}.jsonl.
func TelemetryDir() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "telemetry"), nil
}

// TelemetryServerDir returns the per-server directory under TelemetryDir for
// the given stack and server.
func TelemetryServerDir(stackName, serverName string) (string, error) {
	dir, err := TelemetryDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, stackName, serverName), nil
}

// TelemetryServerPath returns the path to a single signal file for a server.
// signal must be "logs", "metrics", or "traces"; any string is accepted but
// only those three are produced by the daemon.
func TelemetryServerPath(stackName, serverName, signal string) (string, error) {
	dir, err := TelemetryServerDir(stackName, serverName)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, signal+".jsonl"), nil
}

// EnsureTelemetryServerDir creates the per-server telemetry directory with
// mode 0700, matching the vault/state convention. Any new directories on the
// path inherit the same restrictive permissions because lumberjack will not
// chmod them on its own.
func EnsureTelemetryServerDir(stackName, serverName string) error {
	dir, err := TelemetryServerDir(stackName, serverName)
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0700)
}

// WithLock executes fn while holding an exclusive lock on the stack state.
// Returns error if lock cannot be acquired within timeout.
func WithLock(name string, timeout time.Duration, fn func() error) error {
	dir, err := StateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}

	lockPath, err := LockPath(name)
	if err != nil {
		return err
	}
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("opening lock file: %w", err)
	}
	defer lockFile.Close()

	// Try to acquire lock with timeout
	deadline := time.Now().Add(timeout)
	var unlock func() error
	for {
		unlock, err = tryFileLock(lockFile)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout acquiring state lock for %s (another operation may be in progress)", name)
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Lock acquired - ensure we unlock before closing file
	defer func() { _ = unlock() }()

	return fn()
}
