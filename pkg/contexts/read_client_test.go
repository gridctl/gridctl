package contexts

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadClientContextTable(t *testing.T) {
	plain := "hello world\n"
	sourceBody := "<!-- Source: fragments/00-default.md -->\nkept"
	blockOnly := beginMarker + "\n" + blockHeader + "\n\nblock body\n" + endMarker + "\n"
	around := "before text\n\n" + beginMarker + "\n" + blockHeader + "\n\nblock body\n" + endMarker + "\n\nafter text\n"
	withSource := beginMarker + "\n" + blockHeader + "\n\n" + sourceBody + "\n" + endMarker + "\n"
	dedicated := fileHeader + "\n\nroo body\n"
	vscodeBody := "---\napplyTo: \"**\"\n---\n" + fileHeader + "\n\nvscode body\n"
	crlf := "line one\r\nline two\r\n"
	malformed := beginMarker + "\n" + blockHeader + "\nkept body\n"
	emptyBlock := beginMarker + "\n" + blockHeader + "\n\n" + endMarker + "\n"

	cases := []struct {
		name        string
		slug        string
		detect      string
		rel         string
		body        string
		want        string
		nothing     bool
		missing     bool
		unsupported bool
		malformed   bool
		shim        bool
	}{
		{name: "plain", slug: "opencode", detect: ".config/opencode", rel: ".config/opencode/AGENTS.md", body: plain, want: "hello world\n"},
		{name: "block only", slug: "opencode", detect: ".config/opencode", rel: ".config/opencode/AGENTS.md", body: blockOnly, want: "block body\n"},
		{name: "text around block", slug: "opencode", detect: ".config/opencode", rel: ".config/opencode/AGENTS.md", body: around, want: "before text\n\nafter text\n\nblock body\n"},
		{name: "source comment retained", slug: "opencode", detect: ".config/opencode", rel: ".config/opencode/AGENTS.md", body: withSource, want: sourceBody + "\n"},
		{name: "shim plus user text", slug: "gemini", detect: ".gemini", rel: ".gemini/GEMINI.md", body: "USER", shim: true, want: "# My rules\n"},
		{name: "shim only", slug: "gemini", detect: ".gemini", rel: ".gemini/GEMINI.md", shim: true, nothing: true},
		{name: "empty block", slug: "opencode", detect: ".config/opencode", rel: ".config/opencode/AGENTS.md", body: emptyBlock, nothing: true},
		{name: "header only", slug: "opencode", detect: ".config/opencode", rel: ".config/opencode/AGENTS.md", body: fileHeader + "\n", nothing: true},
		{name: "empty file", slug: "opencode", detect: ".config/opencode", rel: ".config/opencode/AGENTS.md", body: "", nothing: true},
		{name: "dedicated header and body", slug: "roo", detect: ".roo", rel: ".roo/rules/gridctl.md", body: dedicated, want: "roo body\n"},
		{name: "vscode frontmatter", slug: "vscode", detect: ".copilot", rel: ".copilot/instructions/gridctl.instructions.md", body: vscodeBody, want: "vscode body\n"},
		{name: "crlf", slug: "opencode", detect: ".config/opencode", rel: ".config/opencode/AGENTS.md", body: crlf, want: "line one\nline two\n"},
		{name: "begin without end", slug: "opencode", detect: ".config/opencode", rel: ".config/opencode/AGENTS.md", body: malformed, malformed: true, want: beginMarker + "\nkept body"},
		{name: "missing", slug: "opencode", detect: ".config/opencode", rel: ".config/opencode/AGENTS.md", missing: true},
		{name: "unsupported", slug: "cursor", unsupported: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestManager(t, tc.detect)
			other := newTestManager(t, tc.detect)
			path := ""
			if tc.rel != "" {
				path = filepath.Join(m.home, tc.rel)
			}
			if !tc.missing && !tc.unsupported {
				body := tc.body
				if tc.shim {
					if tc.nothing {
						body = shimLine(m.CanonicalPath()) + "\n"
					} else {
						body = shimLine(m.CanonicalPath()) + "\n\n# My rules\n"
					}
				}
				writeFile(t, path, body)
				if !tc.malformed && !tc.nothing {
					otherBody := tc.body
					if tc.shim {
						otherBody = shimLine(other.CanonicalPath()) + "\n\n# My rules\n"
					}
					writeFile(t, filepath.Join(other.home, tc.rel), otherBody)
				}
			}

			got, gotPath, err := m.ReadClientContext(context.Background(), tc.slug)
			switch {
			case tc.missing:
				if !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("err = %v, want fs.ErrNotExist", err)
				}
				if gotPath != path {
					t.Fatalf("path = %q, want %q", gotPath, path)
				}
				return
			case tc.unsupported:
				if !errors.Is(err, ErrUnsupported) {
					t.Fatalf("err = %v, want ErrUnsupported", err)
				}
				return
			case tc.nothing:
				if !errors.Is(err, ErrNothingToImport) {
					t.Fatalf("err = %v, want ErrNothingToImport", err)
				}
				if !strings.Contains(err.Error(), "empty after removing gridctl-managed content; nothing to import") {
					t.Fatalf("message = %q", err.Error())
				}
				if gotPath == "" {
					t.Fatal("path empty on nothing-to-import")
				}
				return
			case err != nil:
				t.Fatal(err)
			}

			if tc.malformed {
				if got != tc.want {
					t.Fatalf("malformed content = %q, want %q", got, tc.want)
				}
				if err := other.InitFromClient(tc.slug, false); err == nil {
					t.Fatal("InitFromClient wrote a malformed block")
				}
				return
			}

			norm := strings.TrimRight(normalizeNewlines(got), "\n") + "\n"
			if norm != tc.want {
				t.Fatalf("normalized content = %q, want %q", norm, tc.want)
			}
			if err := other.InitFromClient(tc.slug, false); err != nil {
				t.Fatal(err)
			}
			written, err := other.CanonicalContent()
			if err != nil {
				t.Fatal(err)
			}
			if written != norm {
				t.Fatalf("InitFromClient wrote %q, read normalized %q", written, norm)
			}
		})
	}
}

func TestReadClientContextCancelled(t *testing.T) {
	m := newTestManager(t, ".config/opencode")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := m.ReadClientContext(ctx, "opencode")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestValidateCanonicalContent(t *testing.T) {
	for _, forbidden := range []string{beginMarker, endMarker, headerPrefix} {
		err := ValidateCanonicalContent("prose\n" + forbidden + "\nmore")
		if err == nil {
			t.Fatalf("accepted %q", forbidden)
		}
		if !strings.Contains(err.Error(), `"`+forbidden+`"`) {
			t.Fatalf("message %q does not quote %q", err.Error(), forbidden)
		}
	}
	if err := ValidateCanonicalContent("plain prose without markers\n"); err != nil {
		t.Fatal(err)
	}
}

func TestInitFromClientPreservesBlockBody(t *testing.T) {
	blockOnly := beginMarker + "\n" + blockHeader + "\n\nblock body\n" + endMarker + "\n"
	around := "before text\n\n" + beginMarker + "\n" + blockHeader + "\n\nblock body\n" + endMarker + "\n\nafter text\n"

	t.Run("block only", func(t *testing.T) {
		m := newTestManager(t, ".config/opencode")
		writeFile(t, filepath.Join(m.home, ".config/opencode/AGENTS.md"), blockOnly)
		if err := m.InitFromClient("opencode", false); err != nil {
			t.Fatal(err)
		}
		got, err := m.CanonicalContent()
		if err != nil {
			t.Fatal(err)
		}
		if got != "block body\n" {
			t.Fatalf("canonical = %q", got)
		}
	})

	t.Run("text around block", func(t *testing.T) {
		m := newTestManager(t, ".config/opencode")
		writeFile(t, filepath.Join(m.home, ".config/opencode/AGENTS.md"), around)
		if err := m.InitFromClient("opencode", false); err != nil {
			t.Fatal(err)
		}
		got, err := m.CanonicalContent()
		if err != nil {
			t.Fatal(err)
		}
		want := "before text\n\nafter text\n\nblock body\n"
		if got != want {
			t.Fatalf("canonical = %q, want %q", got, want)
		}
	})
}
