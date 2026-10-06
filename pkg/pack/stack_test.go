package pack

import (
	"strings"
	"testing"
)

func TestParse_StackPath(t *testing.T) {
	base := validManifest + "stack: stacks/team.yaml\n"
	m, err := Parse([]byte(base))
	if err != nil {
		t.Fatal(err)
	}
	if m.Stack != "stacks/team.yaml" {
		t.Fatalf("stack = %q", m.Stack)
	}

	empty := validManifest
	m, err = Parse([]byte(empty))
	if err != nil {
		t.Fatal(err)
	}
	if m.Stack != "" {
		t.Fatalf("empty stack = %q", m.Stack)
	}

	for _, bad := range []string{"/abs.yaml", "../out.yaml", "foo/../../x.yaml", "foo/../x.yaml", `dir\file.yaml`, ".."} {
		src := validManifest + "stack: " + bad + "\n"
		_, err := Parse([]byte(src))
		if err == nil || !strings.Contains(err.Error(), "must be a relative path inside the pack repository") {
			t.Errorf("stack %q: got %v", bad, err)
		}
	}
}
