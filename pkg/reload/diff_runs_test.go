package reload

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

func TestComputeDiff_RunsChanged(t *testing.T) {
	old := &config.Stack{Name: "s"}
	new := &config.Stack{Name: "s", Runs: &config.RunsConfig{Enabled: true}}
	diff := ComputeDiff(old, new)
	if !diff.RunsChanged {
		t.Fatal("expected RunsChanged")
	}
	if diff.IsEmpty() {
		t.Fatal("diff should not be empty")
	}
}

func TestComputeDiff_RunsUnchanged(t *testing.T) {
	old := &config.Stack{Name: "s", Runs: &config.RunsConfig{Enabled: true}}
	new := &config.Stack{Name: "s", Runs: &config.RunsConfig{Enabled: true}}
	diff := ComputeDiff(old, new)
	if diff.RunsChanged {
		t.Fatal("did not expect RunsChanged")
	}
}
