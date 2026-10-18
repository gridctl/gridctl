package packops

import (
	"path/filepath"
	"testing"

	"github.com/gridctl/gridctl/pkg/skills"
)

func TestPinnedStackPath(t *testing.T) {
	if got := PinnedStackPath(nil); got != "" {
		t.Fatalf("nil = %q", got)
	}
	got := PinnedStackPath(&skills.LockedStack{CheckoutDir: "/packs/team/ab12", Path: "stacks/netops.yaml"})
	want := filepath.Join("/packs/team/ab12", "stacks", "netops.yaml")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}
