package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/skillpins"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/versions"
)

const (
	// schemaProbeTimeout bounds `info --json` on a newly downloaded
	// binary. A store hit is not executed.
	schemaProbeTimeout = 10 * time.Second
	// schemaProbeMaxBytes caps the probe stdout. A larger stream is treated
	// as unknown rather than parsed from a prefix.
	schemaProbeMaxBytes = 64 << 10
)

// currentSchemas is the schema advertisement of this process. The replaced
// binary is recorded from these constants; a downloaded binary is probed.
func currentSchemas() *versions.Schemas {
	return &versions.Schemas{
		DaemonState: state.DaemonStateSchemaVersion,
		ProjectLock: project.LockVersion,
		ImportLock:  skills.ImportLockVersion,
		ToolPins:    pins.FileVersion,
		SkillPins:   skillpins.FileVersion,
	}
}

// probeBinarySchemas runs `<path> info --json` and returns the advertised
// schemas. A missing schemas object, a non-zero exit, a timeout, or output
// over the cap records nil (unknown). A present object that lacks tool_pins
// or skill_pins leaves those fields empty, which the pre-swap check treats
// as unknown rather than version zero.
//
// The child environment is only PATH, HOME, and GRIDCTL_HOME. home is the
// resolved gridctl home. An empty home does not probe.
func probeBinarySchemas(ctx context.Context, path, home string) *versions.Schemas {
	if path == "" || home == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, schemaProbeTimeout)
	defer cancel()

	// #nosec G204 -- path is a retained or just-extracted gridctl binary; the argv is fixed.
	cmd := exec.CommandContext(ctx, path, "info", "--json")
	cmd.Env = probeEnv(home)
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil
	}
	if err := cmd.Start(); err != nil {
		return nil
	}
	var buf bytes.Buffer
	n, copyErr := io.Copy(&buf, io.LimitReader(stdout, schemaProbeMaxBytes+1))
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()
	if copyErr != nil || waitErr != nil || n > schemaProbeMaxBytes {
		return nil
	}
	var doc struct {
		Schemas *versions.Schemas `json:"schemas"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		return nil
	}
	return doc.Schemas
}

// probeEnv is the reduced environment for a schema probe. Inherited values
// other than PATH and HOME are dropped so a stored binary cannot see
// credentials. HOME is forwarded only; this function does not use it as a
// gridctl path (homeguard allowlists this file for that reason).
// GRIDCTL_HOME is the caller's resolved home, not whatever the parent
// happened to export.
func probeEnv(home string) []string {
	env := make([]string, 0, 3)
	if path := os.Getenv("PATH"); path != "" {
		env = append(env, "PATH="+path)
	}
	if h := os.Getenv("HOME"); h != "" {
		env = append(env, "HOME="+h)
	}
	if home != "" {
		env = append(env, "GRIDCTL_HOME="+home)
	}
	return env
}
