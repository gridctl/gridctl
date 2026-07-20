package catalog

import (
	"strings"
	"testing"
)

// TestCuratedCatalogValid is the CI gate on the embedded catalog: it must
// parse, every entry must have a sane shape, names must be unique, and no
// secret material may ride along in defaults.
func TestCuratedCatalogValid(t *testing.T) {
	entries, err := Curated()
	if err != nil {
		t.Fatalf("embedded catalog does not parse: %v", err)
	}
	if len(entries) < 10 {
		t.Fatalf("curated catalog has %d entries; want at least 10", len(entries))
	}

	seen := make(map[string]bool)
	for _, e := range entries {
		if e.Name == "" || e.Description == "" {
			t.Errorf("entry %+v: name and description are required", e)
		}
		if seen[strings.ToLower(e.Name)] {
			t.Errorf("duplicate curated entry name %q", e.Name)
		}
		seen[strings.ToLower(e.Name)] = true
		if e.Tier != TierCurated {
			t.Errorf("%s: tier = %q, want %q", e.Name, e.Tier, TierCurated)
		}
		if e.Status != StatusActive {
			t.Errorf("%s: curated entries must be active, got %q", e.Name, e.Status)
		}
		if e.Unsupported != "" {
			t.Errorf("%s: curated entries must be installable, got unsupported %q", e.Name, e.Unsupported)
		}

		switch e.Install.Type {
		case InstallImage:
			if e.Install.Image == "" {
				t.Errorf("%s: image install without an image", e.Name)
			}
		case InstallCommand:
			if len(e.Install.Command) == 0 {
				t.Errorf("%s: command install without a command", e.Name)
			}
			if e.Install.Transport != "stdio" {
				t.Errorf("%s: command installs are stdio, got %q", e.Name, e.Install.Transport)
			}
		case InstallURL:
			if !strings.HasPrefix(e.Install.URL, "https://") {
				t.Errorf("%s: url install must use https, got %q", e.Name, e.Install.URL)
			}
			if e.Install.Transport != "http" && e.Install.Transport != "sse" {
				t.Errorf("%s: url transport %q, want http or sse", e.Name, e.Install.Transport)
			}
		default:
			t.Errorf("%s: unknown install type %q", e.Name, e.Install.Type)
		}

		for _, in := range e.Inputs {
			if in.Name == "" {
				t.Errorf("%s: input without a name", e.Name)
			}
			if in.Secret && in.Default != "" {
				t.Errorf("%s: secret input %s carries a default value", e.Name, in.Name)
			}
		}
	}
}

func TestFindCurated(t *testing.T) {
	if _, ok := FindCurated("github"); !ok {
		t.Fatal("github should be in the curated catalog")
	}
	if _, ok := FindCurated("GitHub"); !ok {
		t.Fatal("curated lookup should be case-insensitive")
	}
	if _, ok := FindCurated("no-such-server"); ok {
		t.Fatal("unknown name should not resolve")
	}
}

func TestFilterCurated(t *testing.T) {
	all, err := FilterCurated("")
	if err != nil {
		t.Fatal(err)
	}
	matched, err := FilterCurated("browser")
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) == 0 || len(matched) >= len(all) {
		t.Fatalf("substring filter returned %d of %d entries", len(matched), len(all))
	}
	for _, e := range matched {
		text := strings.ToLower(e.Name + e.Title + e.Description)
		if !strings.Contains(text, "browser") {
			t.Errorf("%s does not match query %q", e.Name, "browser")
		}
	}
}

func TestMerge_CuratedWinsAndSorts(t *testing.T) {
	curated := []Entry{
		{Name: "github", Namespace: "io.github.github/github-mcp-server", Tier: TierCurated},
	}
	registry := []Entry{
		{Name: "io.github.zeta/z-server", Tier: TierRegistry},
		{Name: "io.github.github/github-mcp-server", Tier: TierRegistry},
		{Name: "io.github.alpha/a-server", Tier: TierRegistry},
	}
	merged := Merge(curated, registry)
	if len(merged) != 3 {
		t.Fatalf("merged %d entries, want 3 (registry duplicate dropped)", len(merged))
	}
	if merged[0].Name != "github" {
		t.Errorf("curated entry should sort first, got %q", merged[0].Name)
	}
	if merged[1].Name != "io.github.alpha/a-server" || merged[2].Name != "io.github.zeta/z-server" {
		t.Errorf("registry entries not sorted: %q, %q", merged[1].Name, merged[2].Name)
	}
}
