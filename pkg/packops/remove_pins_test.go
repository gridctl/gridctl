package packops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/skillpins"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/state"
)

func seedPinnedSkillPack(t *testing.T) (*Managers, *skills.Importer, string) {
	t.Helper()
	t.Setenv("GRIDCTL_HOME", "")
	mgrs, imp := testEnv(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRIDCTL_HOME", home)
	lf := &skills.LockFile{Sources: map[string]skills.LockedSource{
		"pin-pack": {
			Repo: "https://example.com/pin-pack.git",
			Pack: &skills.LockedPack{Name: "pin-pack", Skills: []string{"alpha"}},
		},
	}}
	if err := skills.WriteLockFile(skills.LockFilePath(), lf); err != nil {
		t.Fatal(err)
	}
	imp.SetSkillPinResetter(func(ctx context.Context, name string) ([]string, error) {
		reset, _, err := skillpins.ResetOnDisk(ctx, name, nil)
		return reset, err
	})
	return mgrs, imp, home
}

func writePackPin(t *testing.T, stack, skill string) (string, []byte) {
	t.Helper()
	path, err := state.SkillPinsPath(stack)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	pf := &skillpins.PinFile{
		Version:   "1",
		Stack:     stack,
		CreatedAt: when,
		Skills: map[string]*skillpins.SkillPin{
			skill: {
				SkillHash:      "abc",
				Status:         skillpins.StatusPinned,
				PinnedAt:       when,
				LastVerifiedAt: when,
			},
		},
	}
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, data
}

func skillRow(t *testing.T, doc *RemoveDoc, name string) Row {
	t.Helper()
	for _, row := range doc.Rows {
		if row.Kind == "skill" && row.Name == name {
			return row
		}
	}
	t.Fatalf("no skill row for %s in %+v", name, doc.Rows)
	return Row{}
}

func TestRemove_SkillRowReportsPinReset(t *testing.T) {
	mgrs, imp, _ := seedPinnedSkillPack(t)
	_, _ = writePackPin(t, "daily", "alpha")
	_, _ = writePackPin(t, "other", "alpha")

	doc, err := mgrs.Remove(t.Context(), imp, "pin-pack", RemoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	row := skillRow(t, doc, "alpha")
	if row.Action != "removed" {
		t.Fatalf("action = %q, want removed", row.Action)
	}
	if row.Detail != "registry entry removed, pin reset (daily, other)" {
		t.Fatalf("detail = %q", row.Detail)
	}
}

func TestRemove_DryRunPreviewsPinReset(t *testing.T) {
	mgrs, imp, _ := seedPinnedSkillPack(t)
	path, before := writePackPin(t, "daily", "alpha")

	doc, err := mgrs.Remove(t.Context(), imp, "pin-pack", RemoveOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	row := skillRow(t, doc, "alpha")
	if row.Action != "would-remove" {
		t.Fatalf("action = %q, want would-remove", row.Action)
	}
	if row.Detail != "would reset pin (daily)" {
		t.Fatalf("detail = %q", row.Detail)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("dry run rewrote the pin file")
	}
}
