package main

import (
	"encoding/json"
	"testing"

	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/skillpins"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/state"
)

func TestInfoSchemasMatchConstants(t *testing.T) {
	schemas := advertisedInfoSchemas()
	if schemas == nil {
		t.Fatal("schemas is nil")
	}
	if schemas.DaemonState != state.DaemonStateSchemaVersion {
		t.Errorf("daemon_state = %d, want %d", schemas.DaemonState, state.DaemonStateSchemaVersion)
	}
	if schemas.ProjectLock != project.LockVersion {
		t.Errorf("project_lock = %d, want %d", schemas.ProjectLock, project.LockVersion)
	}
	if schemas.ImportLock != skills.ImportLockVersion {
		t.Errorf("import_lock = %d, want %d", schemas.ImportLock, skills.ImportLockVersion)
	}
	if schemas.ToolPins != pins.FileVersion {
		t.Errorf("tool_pins = %q, want %q", schemas.ToolPins, pins.FileVersion)
	}
	if schemas.SkillPins != skillpins.FileVersion {
		t.Errorf("skill_pins = %q, want %q", schemas.SkillPins, skillpins.FileVersion)
	}

	// Both output paths carry the same object. The error path is the
	// runtime-detection failure; the success path is a detected runtime.
	errDoc := infoJSON{Error: "runtime not detected", Schemas: advertisedInfoSchemas()}
	okDoc := infoJSON{Runtime: "docker", Schemas: advertisedInfoSchemas()}
	for _, doc := range []infoJSON{errDoc, okDoc} {
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Schemas infoSchemas `json:"schemas"`
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got.Schemas != *schemas {
			t.Fatalf("encoded schemas = %+v, want %+v", got.Schemas, *schemas)
		}
	}
	line := formatInfoSchemasLine(schemas)
	if line == "" || line[:8] != "Schemas:" {
		t.Fatalf("text line = %q", line)
	}
}
