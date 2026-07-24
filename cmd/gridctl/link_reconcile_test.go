package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/provisioner"
)

// reconcileFake is a configurable ClientProvisioner for reconcile tests;
// the shared fakeProvisioner is too rigid (always detected, never errors).
type reconcileFake struct {
	slug      string
	detected  bool
	linkErr   error
	unlinkErr error
	isLinked  bool
	bridge    bool

	linkCalls   []provisioner.LinkOptions
	unlinkNames []string
}

func (f *reconcileFake) Name() string { return f.slug }
func (f *reconcileFake) Slug() string { return f.slug }
func (f *reconcileFake) Detect() (string, bool) {
	if !f.detected {
		return "", false
	}
	return "/nonexistent/" + f.slug + ".json", true
}
func (f *reconcileFake) IsLinked(string, string) (bool, error) { return f.isLinked, nil }
func (f *reconcileFake) Link(_ string, opts provisioner.LinkOptions) error {
	f.linkCalls = append(f.linkCalls, opts)
	return f.linkErr
}
func (f *reconcileFake) Unlink(_ string, serverName string) error {
	f.unlinkNames = append(f.unlinkNames, serverName)
	return f.unlinkErr
}
func (f *reconcileFake) NeedsBridge() bool { return f.bridge }
func (f *reconcileFake) ListServers(string) ([]provisioner.ServerEntry, error) {
	return nil, nil
}

// fakeResolver maps slugs to fakes, standing in for the provisioner
// registry.
type fakeResolver map[string]*reconcileFake

func (r fakeResolver) FindBySlug(slug string) (provisioner.ClientProvisioner, bool) {
	f, ok := r[slug]
	return f, ok
}

func TestReconcileDeclaredLinks(t *testing.T) {
	t.Run("links declared clients with resolved options", func(t *testing.T) {
		claude := &reconcileFake{slug: "claude", detected: true}
		cursor := &reconcileFake{slug: "cursor", detected: true}
		resolver := fakeResolver{"claude": claude, "cursor": cursor}

		var buf bytes.Buffer
		entries := []config.LinkEntry{
			{Client: "claude"},
			{Client: "cursor", Group: "dev", ClientID: "cursor"},
		}
		reconcileDeclaredLinks(output.NewWithWriter(&buf), resolver, entries, 8181, false)

		if len(claude.linkCalls) != 1 || len(cursor.linkCalls) != 1 {
			t.Fatalf("link calls: claude=%d cursor=%d", len(claude.linkCalls), len(cursor.linkCalls))
		}
		if got := claude.linkCalls[0].ServerName; got != "gridctl" {
			t.Errorf("claude server name = %q, want gridctl", got)
		}
		opts := cursor.linkCalls[0]
		if opts.ServerName != "gridctl-dev" {
			t.Errorf("group entry server name = %q, want gridctl-dev", opts.ServerName)
		}
		if !strings.Contains(opts.GatewayURL, "/groups/dev/") {
			t.Errorf("group entry URL = %q, want group endpoint", opts.GatewayURL)
		}
		if !strings.Contains(opts.GatewayURL, "client=cursor") {
			t.Errorf("client_id not on URL: %q", opts.GatewayURL)
		}
	})

	t.Run("not detected warns and skips", func(t *testing.T) {
		missing := &reconcileFake{slug: "zed", detected: false}
		var buf bytes.Buffer
		reconcileDeclaredLinks(output.NewWithWriter(&buf), fakeResolver{"zed": missing}, []config.LinkEntry{{Client: "zed"}}, 8180, false)
		if len(missing.linkCalls) != 0 {
			t.Errorf("undetected client must not be linked")
		}
	})

	t.Run("already linked is a silent no-op", func(t *testing.T) {
		f := &reconcileFake{slug: "claude", detected: true, linkErr: provisioner.ErrAlreadyLinked}
		var buf bytes.Buffer
		reconcileDeclaredLinks(output.NewWithWriter(&buf), fakeResolver{"claude": f}, []config.LinkEntry{{Client: "claude"}}, 8180, false)
		if strings.Contains(buf.String(), "already") {
			t.Errorf("already-linked should not print: %q", buf.String())
		}
	})

	t.Run("conflict warns with force hint and continues", func(t *testing.T) {
		conflicted := &reconcileFake{slug: "claude", detected: true, linkErr: provisioner.ErrConflict}
		next := &reconcileFake{slug: "cursor", detected: true}
		var buf bytes.Buffer
		entries := []config.LinkEntry{{Client: "claude"}, {Client: "cursor"}}
		reconcileDeclaredLinks(output.NewWithWriter(&buf), fakeResolver{"claude": conflicted, "cursor": next}, entries, 8180, false)
		if !strings.Contains(buf.String(), "--force") {
			t.Errorf("conflict warning must carry the force hint: %q", buf.String())
		}
		if len(next.linkCalls) != 1 {
			t.Errorf("conflict must not stop later entries")
		}
	})

	t.Run("quiet keeps warnings, drops success lines", func(t *testing.T) {
		ok := &reconcileFake{slug: "claude", detected: true}
		missing := &reconcileFake{slug: "zed", detected: false}
		var buf bytes.Buffer
		entries := []config.LinkEntry{{Client: "claude"}, {Client: "zed"}}
		reconcileDeclaredLinks(output.NewWithWriter(&buf), fakeResolver{"claude": ok, "zed": missing}, entries, 8180, true)
		out := buf.String()
		if strings.Contains(out, "Linked") {
			t.Errorf("quiet must suppress success lines: %q", out)
		}
		if !strings.Contains(out, "not detected") {
			t.Errorf("quiet must keep warnings: %q", out)
		}
	})
}

func TestComputeLinkActions(t *testing.T) {
	resolver := fakeResolver{
		"claude": {slug: "claude", detected: true, isLinked: true},
		"cursor": {slug: "cursor", detected: true},
		"zed":    {slug: "zed", detected: false},
	}
	entries := []config.LinkEntry{
		{Client: "claude"},
		{Client: "cursor", Group: "dev"},
		{Client: "zed"},
	}
	actions := computeLinkActions(resolver, entries)
	want := []linkAction{
		{Slug: "claude", Name: "gridctl", Action: "already-linked"},
		{Slug: "cursor", Name: "gridctl-dev", Action: "link"},
		{Slug: "zed", Name: "gridctl", Action: "skip"},
	}
	if len(actions) != len(want) {
		t.Fatalf("got %d actions, want %d", len(actions), len(want))
	}
	for i := range want {
		if actions[i] != want[i] {
			t.Errorf("action[%d] = %+v, want %+v", i, actions[i], want[i])
		}
	}
}

func TestUnlinkDeclaredClients(t *testing.T) {
	t.Run("resolves group entry names", func(t *testing.T) {
		cursor := &reconcileFake{slug: "cursor", detected: true}
		var buf bytes.Buffer
		stack := &config.Stack{Link: []config.LinkEntry{{Client: "cursor", Group: "dev"}}}
		unlinkDeclaredClients(output.NewWithWriter(&buf), fakeResolver{"cursor": cursor}, stack)
		if len(cursor.unlinkNames) != 1 || cursor.unlinkNames[0] != "gridctl-dev" {
			t.Errorf("unlink names = %v, want [gridctl-dev]", cursor.unlinkNames)
		}
	})

	t.Run("nil stack warns and skips", func(t *testing.T) {
		var buf bytes.Buffer
		unlinkDeclaredClients(output.NewWithWriter(&buf), fakeResolver{}, nil)
		if !strings.Contains(buf.String(), "not loadable") {
			t.Errorf("want not-loadable warning, got %q", buf.String())
		}
	})

	t.Run("not linked is silent", func(t *testing.T) {
		f := &reconcileFake{slug: "claude", detected: true, unlinkErr: provisioner.ErrNotLinked}
		var buf bytes.Buffer
		stack := &config.Stack{Link: []config.LinkEntry{{Client: "claude"}}}
		unlinkDeclaredClients(output.NewWithWriter(&buf), fakeResolver{"claude": f}, stack)
		if strings.Contains(buf.String(), "Failed") {
			t.Errorf("ErrNotLinked must be silent: %q", buf.String())
		}
	})
}

func TestLoadDeclaredLinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	src := "version: \"1\"\nname: demo\nlink:\n  - claude\n  - client: cursor\n    group: dev\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	entries := loadDeclaredLinks(path)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[1].Group != "dev" {
		t.Errorf("entry options lost: %+v", entries[1])
	}

	if got := loadDeclaredLinks(filepath.Join(dir, "missing.yaml")); got != nil {
		t.Errorf("missing file must yield nil, got %v", got)
	}
}
