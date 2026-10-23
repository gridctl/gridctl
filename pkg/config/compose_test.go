package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateFragment_RejectsRequires(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "servers.yaml")
	body := "version: \"1\"\nrequires:\n  gridctl: \">=1.1.0\"\nmcp-servers:\n  - name: tools\n    command: [echo]\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ValidateFragment(path)
	if err == nil || !strings.Contains(err.Error(), "'requires' is not allowed in a stack fragment") {
		t.Fatalf("err = %v", err)
	}
}

func TestCompose_PreservesBaseRequires(t *testing.T) {
	ResetRunningVersion()
	t.Cleanup(ResetRunningVersion)
	SetRunningVersion("1.0.0")

	base := &Stack{
		Name:       "infra",
		Requires:   &RequiresConfig{Gridctl: ">=1.1.0"},
		MCPServers: []MCPServer{{Name: "base", Command: []string{"echo"}}},
	}
	frag := &Stack{MCPServers: []MCPServer{{Name: "extra", Command: []string{"echo"}}}}
	out, err := Compose(
		Layer{Pack: "infra", Path: "stack.yaml", Dir: t.TempDir(), Stack: base},
		[]Layer{{Pack: "netops", Path: "servers.yaml", Dir: t.TempDir(), Stack: frag}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if out.Requires == nil || out.Requires.Gridctl != base.Requires.Gridctl {
		t.Fatalf("composed requires = %+v", out.Requires)
	}
	if base.Requires.Gridctl != ">=1.1.0" {
		t.Fatal("compose mutated the base requires")
	}
	result := ValidateWithIssues(out)
	found := false
	for _, issue := range result.Issues {
		if issue.Field == "requires.gridctl" && issue.Severity == SeverityError {
			found = true
		}
	}
	if !found {
		t.Fatalf("composed validation = %+v", result.Issues)
	}
	rendered, err := RenderComposed(out, []Layer{{Pack: "infra", Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Path: "stack.yaml"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rendered), ">=1.1.0") {
		t.Fatalf("rendered composed file dropped requires:\n%s", rendered)
	}
	path := filepath.Join(t.TempDir(), "stack.yaml")
	if err := WriteComposed(path, out, []Layer{{Pack: "infra", Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Path: "stack.yaml"}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "requires:") || !strings.Contains(string(raw), ">=1.1.0") {
		t.Fatalf("composed file = %s", raw)
	}
}

func TestValidateFragment_Allowlist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "servers.yaml")
	body := "version: \"1\"\nname: should-fail\ngateway:\n  port: 1\nmcp-servers:\n  - name: tools\n    command: [echo]\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ValidateFragment(path)
	if err == nil || !strings.Contains(err.Error(), "'gateway' is not allowed") || !strings.Contains(err.Error(), "fragments may declare mcp-servers, resources, and variables only") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateFragment_ExtendsAndEmpty(t *testing.T) {
	dir := t.TempDir()
	extends := filepath.Join(dir, "extends.yaml")
	if err := os.WriteFile(extends, []byte("version: \"1\"\nextends: parent.yaml\nmcp-servers:\n  - name: tools\n    command: [echo]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ValidateFragment(extends)
	if err == nil || !strings.Contains(err.Error(), "'extends' is not allowed") {
		t.Fatalf("extends err = %v", err)
	}
	empty := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(empty, []byte("version: \"1\"\nvariables:\n  TOKEN:\n    required: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = ValidateFragment(empty)
	if err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("empty err = %v", err)
	}
}

func TestCompose_CollisionNamesBothOwners(t *testing.T) {
	base := &Stack{Name: "infra", MCPServers: []MCPServer{{Name: "netbox", Command: []string{"echo"}}}}
	frag := &Stack{MCPServers: []MCPServer{{Name: "netbox", Command: []string{"echo"}}}}
	_, err := Compose(Layer{Pack: "infra", Stack: base, Dir: t.TempDir()}, []Layer{{Pack: "netops-team", Stack: frag, Dir: t.TempDir()}})
	if err == nil || err.Error() != "server 'netbox' is declared by pack 'infra' and pack 'netops-team'" {
		t.Fatalf("err = %v", err)
	}
}

func TestCompose_RewritesRelativePaths(t *testing.T) {
	baseDir := t.TempDir()
	fragDir := t.TempDir()
	base := &Stack{Name: "infra", MCPServers: []MCPServer{{
		Name: "base", Command: []string{"echo"},
		Source: &Source{Type: "local", Path: "src"},
	}}}
	frag := &Stack{MCPServers: []MCPServer{{
		Name: "extra", Command: []string{"echo"},
		Source:  &Source{Type: "local", Path: "./tools"},
		Volumes: []string{"./data:/data"},
	}}}
	out, err := Compose(
		Layer{Pack: "infra", Path: "stack.yaml", Dir: baseDir, Stack: base},
		[]Layer{{Pack: "netops", Path: "servers.yaml", Dir: fragDir, Stack: frag}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if out.MCPServers[0].Source.Path != filepath.Join(baseDir, "src") {
		t.Fatalf("base path = %s", out.MCPServers[0].Source.Path)
	}
	if out.MCPServers[1].Source.Path != filepath.Join(fragDir, "tools") {
		t.Fatalf("fragment path = %s", out.MCPServers[1].Source.Path)
	}
	if !strings.HasPrefix(out.MCPServers[1].Volumes[0], fragDir) {
		t.Fatalf("volume = %s", out.MCPServers[1].Volumes[0])
	}
	if base.MCPServers[0].Source.Path != "src" {
		t.Fatal("compose mutated the base stack")
	}
}

func TestComposedHeaderRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	stack := &Stack{Version: "1", Name: "infra", MCPServers: []MCPServer{{Name: "base", Command: []string{"echo"}}}}
	layers := []Layer{
		{Pack: "infra", Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Path: "stack.yaml"},
		{Pack: "netops-team", Commit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Path: "servers.yaml"},
	}
	if err := WriteComposed(path, stack, layers); err != nil {
		t.Fatal(err)
	}
	got, ok, err := ReadComposedHeader(path)
	if err != nil || !ok {
		t.Fatalf("header ok=%v err=%v", ok, err)
	}
	if len(got) != 2 || got[0].Pack != "infra" || got[1].Pack != "netops-team" || got[1].Path != "servers.yaml" {
		t.Fatalf("layers = %+v", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "T") && strings.Contains(string(raw), "Z") {
		t.Fatalf("header has a timestamp:\n%s", raw)
	}
	again, err := RenderComposed(stack, layers)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(again) {
		t.Fatal("render is not stable")
	}
	plain := filepath.Join(dir, "plain.yaml")
	if err := os.WriteFile(plain, []byte("version: \"1\"\nname: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := ReadComposedHeader(plain); err != nil || ok {
		t.Fatalf("plain header ok=%v err=%v", ok, err)
	}
}
