package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/state"
)

func TestProbeBinarySchemas(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SECRET_TOKEN", "super-secret")

	t.Run("parses schemas and drops other env", func(t *testing.T) {
		envFile := filepath.Join(t.TempDir(), "env")
		script := writeProbeScript(t, "#!/bin/sh\nenv > "+shellQuote(envFile)+"\nprintf '%s\\n' '{\"schemas\":{\"daemon_state\":2,\"project_lock\":1,\"import_lock\":8,\"tool_pins\":\"2\",\"skill_pins\":\"1\"}}'\n")
		got := probeBinarySchemas(t.Context(), script, home)
		if got == nil || got.DaemonState != 2 || got.ToolPins != "2" || got.SkillPins != "1" {
			t.Fatalf("schemas = %+v", got)
		}
		env, err := os.ReadFile(envFile)
		if err != nil {
			t.Fatal(err)
		}
		text := string(env)
		if strings.Contains(text, "SECRET_TOKEN") || strings.Contains(text, "super-secret") {
			t.Fatalf("probe inherited a secret:\n%s", text)
		}
		if !strings.Contains(text, "GRIDCTL_HOME="+home) {
			t.Fatalf("probe env missing home:\n%s", text)
		}
	})

	t.Run("missing pin fields are empty", func(t *testing.T) {
		script := writeProbeScript(t, "#!/bin/sh\nprintf '%s\\n' '{\"schemas\":{\"daemon_state\":1,\"project_lock\":1,\"import_lock\":1}}'\n")
		got := probeBinarySchemas(t.Context(), script, home)
		if got == nil || got.ToolPins != "" || got.SkillPins != "" || got.DaemonState != 1 {
			t.Fatalf("schemas = %+v", got)
		}
	})

	t.Run("missing schemas object is nil", func(t *testing.T) {
		script := writeProbeScript(t, "#!/bin/sh\nprintf '%s\\n' '{\"runtime\":\"none\"}'\n")
		if got := probeBinarySchemas(t.Context(), script, home); got != nil {
			t.Fatalf("schemas = %+v", got)
		}
	})

	t.Run("non-zero exit is nil", func(t *testing.T) {
		script := writeProbeScript(t, "#!/bin/sh\nexit 1\n")
		if got := probeBinarySchemas(t.Context(), script, home); got != nil {
			t.Fatalf("schemas = %+v", got)
		}
	})

	t.Run("oversized output is nil", func(t *testing.T) {
		script := writeProbeScript(t, "#!/bin/sh\nhead -c 70000 /dev/zero | tr '\\0' 'x'\n")
		if got := probeBinarySchemas(t.Context(), script, home); got != nil {
			t.Fatalf("schemas = %+v", got)
		}
	})

	t.Run("parent deadline wins", func(t *testing.T) {
		script := writeProbeScript(t, "#!/bin/sh\nsleep 30\n")
		ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		defer cancel()
		if got := probeBinarySchemas(ctx, script, home); got != nil {
			t.Fatalf("schemas = %+v", got)
		}
	})
}

func TestCurrentSchemasMatchConstants(t *testing.T) {
	got := currentSchemas()
	if got.DaemonState != state.DaemonStateSchemaVersion || got.ToolPins != pins.FileVersion {
		t.Fatalf("schemas = %+v", got)
	}
	if got.SkillPins == "" {
		t.Fatal("skill pin version empty")
	}
}

func writeProbeScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gridctl")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
