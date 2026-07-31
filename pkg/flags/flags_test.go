package flags

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func experimentalFlag(name string) Flag {
	return Flag{
		Name:        name,
		Description: "test flag",
		Stage:       StageExperimental,
		Since:       "0.1.0",
		GraduatesBy: "0.3.0",
	}
}

func mustRegistry(t *testing.T, entries ...Flag) *Registry {
	t.Helper()
	r, err := NewRegistry(entries...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return r
}

func TestNewRegistry(t *testing.T) {
	tests := []struct {
		name    string
		entries []Flag
		wantErr string
	}{
		{name: "empty registry is valid"},
		{name: "valid experimental flag", entries: []Flag{experimentalFlag("foo_bar")}},
		{
			name: "valid graduated flag",
			entries: []Flag{{
				Name: "old_flag", Description: "d", Stage: StageGraduated,
				Since: "0.1.0", GraduatesBy: "0.2.0",
				Message: "remove the entry, the feature is now always on",
			}},
		},
		{
			name:    "duplicate names rejected",
			entries: []Flag{experimentalFlag("dup"), experimentalFlag("dup")},
			wantErr: "registered twice",
		},
		{
			name:    "empty name rejected",
			entries: []Flag{{Stage: StageExperimental, Since: "0.1.0", GraduatesBy: "0.2.0"}},
			wantErr: "empty name",
		},
		{
			name:    "kebab-case rejected",
			entries: []Flag{experimentalFlag("foo-bar")},
			wantErr: "snake_case",
		},
		{
			name: "experimental without GraduatesBy rejected",
			entries: []Flag{{
				Name: "no_deadline", Stage: StageExperimental, Since: "0.1.0",
			}},
			wantErr: "GraduatesBy is required",
		},
		{
			name: "graduated without Message rejected",
			entries: []Flag{{
				Name: "no_message", Stage: StageGraduated, Since: "0.1.0",
			}},
			wantErr: "Message is required",
		},
		{
			name:    "missing Since rejected",
			entries: []Flag{{Name: "no_since", Stage: StageExperimental, GraduatesBy: "0.2.0"}},
			wantErr: "Since is required",
		},
		{
			name:    "unknown stage rejected",
			entries: []Flag{{Name: "bad_stage", Stage: Stage("beta"), Since: "0.1.0"}},
			wantErr: "unknown stage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRegistry(tt.entries...)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("NewRegistry: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("NewRegistry error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestBuiltinRegistryValid(t *testing.T) {
	if _, err := NewRegistry(builtin...); err != nil {
		t.Fatalf("built-in registry invalid: %v", err)
	}
	if len(Default().All()) != len(builtin) {
		t.Fatal("Default() lost entries")
	}
}

func TestLookupAndAll(t *testing.T) {
	r := mustRegistry(t, experimentalFlag("aaa"), experimentalFlag("bbb"))
	if _, ok := r.Lookup("aaa"); !ok {
		t.Fatal("Lookup(aaa) not found")
	}
	if _, ok := r.Lookup("missing"); ok {
		t.Fatal("Lookup(missing) unexpectedly found")
	}
	all := r.All()
	if len(all) != 2 || all[0].Name != "aaa" || all[1].Name != "bbb" {
		t.Fatalf("All() = %v, want registration order aaa, bbb", all)
	}
	// All returns a copy: mutating it must not corrupt the registry.
	all[0].Name = "mutated"
	if _, ok := r.Lookup("aaa"); !ok {
		t.Fatal("registry mutated through All() result")
	}
}

func TestEnvVar(t *testing.T) {
	f := experimentalFlag("transport_dual_stack")
	if got := f.EnvVar(); got != "GRIDCTL_EXPERIMENTAL_TRANSPORT_DUAL_STACK" {
		t.Fatalf("EnvVar() = %q", got)
	}
}

func TestResolve(t *testing.T) {
	// Article IX back-compat first: an absent map resolves to nothing.
	t.Run("nil map resolves empty", func(t *testing.T) {
		r := mustRegistry(t, experimentalFlag("foo"))
		res := Resolve(r, nil)
		if res.Enabled != nil || len(res.Warnings) != 0 {
			t.Fatalf("Resolve(nil) = %+v, want empty", res)
		}
	})

	t.Run("yaml true enables", func(t *testing.T) {
		r := mustRegistry(t, experimentalFlag("foo"))
		res := Resolve(r, map[string]bool{"foo": true})
		if !res.Enabled["foo"] || len(res.Warnings) != 0 {
			t.Fatalf("Resolve = %+v", res)
		}
	})

	t.Run("yaml false stays off and out of Enabled", func(t *testing.T) {
		r := mustRegistry(t, experimentalFlag("foo"))
		res := Resolve(r, map[string]bool{"foo": false})
		if res.Enabled != nil {
			t.Fatalf("Enabled = %v, want nil", res.Enabled)
		}
	})

	t.Run("unknown name warns and lists valid flags", func(t *testing.T) {
		r := mustRegistry(t, experimentalFlag("foo"))
		res := Resolve(r, map[string]bool{"tpyo": true})
		if res.Enabled != nil {
			t.Fatalf("Enabled = %v, want nil", res.Enabled)
		}
		if len(res.Warnings) != 1 ||
			!strings.Contains(res.Warnings[0].Message, `unknown experimental flag "tpyo"`) ||
			!strings.Contains(res.Warnings[0].Message, "valid flags: foo") {
			t.Fatalf("Warnings = %+v", res.Warnings)
		}
	})

	t.Run("graduated name warns with migration message and is ignored", func(t *testing.T) {
		r := mustRegistry(t, Flag{
			Name: "grown_up", Description: "d", Stage: StageGraduated,
			Since: "0.1.0", GraduatesBy: "0.2.0",
			Message: "graduated in 0.2.0; remove the entry, the feature is now always on",
		})
		res := Resolve(r, map[string]bool{"grown_up": true})
		if res.Enabled != nil {
			t.Fatalf("Enabled = %v, want nil", res.Enabled)
		}
		if len(res.Warnings) != 1 ||
			!strings.Contains(res.Warnings[0].Message, `"grown_up" graduated:`) ||
			!strings.Contains(res.Warnings[0].Message, "always on") {
			t.Fatalf("Warnings = %+v", res.Warnings)
		}
	})

	t.Run("removed name warns with migration message", func(t *testing.T) {
		r := mustRegistry(t, Flag{
			Name: "gone", Description: "d", Stage: StageRemoved,
			Since: "0.1.0", GraduatesBy: "0.2.0", Message: "the experiment was abandoned in 0.3.0",
		})
		res := Resolve(r, map[string]bool{"gone": true})
		if res.Enabled != nil || len(res.Warnings) != 1 ||
			!strings.Contains(res.Warnings[0].Message, `"gone" was removed: the experiment was abandoned in 0.3.0`) {
			t.Fatalf("res = %+v", res)
		}
	})

	t.Run("env override beats yaml true", func(t *testing.T) {
		r := mustRegistry(t, experimentalFlag("foo"))
		t.Setenv("GRIDCTL_EXPERIMENTAL_FOO", "false")
		res := Resolve(r, map[string]bool{"foo": true})
		if res.Enabled != nil {
			t.Fatalf("Enabled = %v, want nil (env false wins)", res.Enabled)
		}
	})

	t.Run("env override enables without yaml", func(t *testing.T) {
		r := mustRegistry(t, experimentalFlag("foo"))
		t.Setenv("GRIDCTL_EXPERIMENTAL_FOO", "1")
		res := Resolve(r, nil)
		if !res.Enabled["foo"] {
			t.Fatalf("Enabled = %v, want foo on", res.Enabled)
		}
	})

	t.Run("malformed env warns and defers to yaml", func(t *testing.T) {
		r := mustRegistry(t, experimentalFlag("foo"))
		t.Setenv("GRIDCTL_EXPERIMENTAL_FOO", "banana")
		res := Resolve(r, map[string]bool{"foo": true})
		if !res.Enabled["foo"] {
			t.Fatal("yaml true must survive a malformed env override")
		}
		if len(res.Warnings) != 1 ||
			!strings.Contains(res.Warnings[0].Message, `GRIDCTL_EXPERIMENTAL_FOO="banana"`) {
			t.Fatalf("Warnings = %+v", res.Warnings)
		}
	})
}

func TestCheckNames_EmptyMap(t *testing.T) {
	r := mustRegistry(t, experimentalFlag("foo"))
	if w := CheckNames(r, nil); w != nil {
		t.Fatalf("CheckNames(nil) = %v, want nil", w)
	}
	if w := CheckNames(r, map[string]bool{}); w != nil {
		t.Fatalf("CheckNames(empty) = %v, want nil", w)
	}
}

func TestCheckNames_DeterministicOrder(t *testing.T) {
	r := mustRegistry(t)
	w := CheckNames(r, map[string]bool{"zzz": true, "aaa": true})
	if len(w) != 2 || w[0].Name != "aaa" || w[1].Name != "zzz" {
		t.Fatalf("warnings not sorted by name: %+v", w)
	}
}

func TestOverdue(t *testing.T) {
	deadline := Flag{
		Name: "late", Description: "d", Stage: StageExperimental,
		Since: "0.1.0", GraduatesBy: "0.2.0",
	}
	fresh := Flag{
		Name: "fresh", Description: "d", Stage: StageExperimental,
		Since: "0.1.0", GraduatesBy: "9.9.9",
	}
	r := mustRegistry(t, deadline, fresh)

	tests := []struct {
		name    string
		version string
		want    []string
	}{
		{name: "before deadline", version: "0.1.9", want: nil},
		{name: "at deadline is overdue", version: "0.2.0", want: []string{"late"}},
		{name: "past deadline", version: "1.0.0", want: []string{"late"}},
		{name: "prerelease of deadline counts", version: "0.2.0-beta.1", want: []string{"late"}},
		{name: "v prefix accepted", version: "v0.2.0", want: []string{"late"}},
		{name: "dev build never enforces", version: "dev", want: nil},
		{name: "empty version never enforces", version: "", want: nil},
		{name: "unparseable version never enforces", version: "nightly", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Overdue(r, tt.version)
			var names []string
			for _, f := range got {
				names = append(names, f.Name)
			}
			if len(names) != len(tt.want) {
				t.Fatalf("Overdue(%q) = %v, want %v", tt.version, names, tt.want)
			}
			for i := range names {
				if names[i] != tt.want[i] {
					t.Fatalf("Overdue(%q) = %v, want %v", tt.version, names, tt.want)
				}
			}
		})
	}

	// Graduated flags are never overdue: the decision was made.
	t.Run("graduated flags exempt", func(t *testing.T) {
		r := mustRegistry(t, Flag{
			Name: "done", Description: "d", Stage: StageGraduated,
			Since: "0.1.0", GraduatesBy: "0.2.0", Message: "always on",
		})
		if got := Overdue(r, "9.9.9"); got != nil {
			t.Fatalf("Overdue = %v, want nil", got)
		}
	})
}

// TestNoBuiltinFlagOverdue is the graduation clock: it fails when a built-in
// experimental flag's GraduatesBy is at or before the most recent release in
// CHANGELOG.md. Fix it by graduating the flag (promote the config, flip the
// Stage, add a Message) or by deliberately extending GraduatesBy in a
// reviewed diff.
func TestNoBuiltinFlagOverdue(t *testing.T) {
	version := latestReleaseVersion(t)
	if version == "" {
		t.Skip("no release heading found in CHANGELOG.md")
	}
	overdue := Overdue(Default(), version)
	for _, f := range overdue {
		t.Errorf("flag %q passed its GraduatesBy deadline (%s, current release %s): graduate it or extend the deadline deliberately",
			f.Name, f.GraduatesBy, version)
	}
}

// latestReleaseVersion parses the newest "## [x.y.z...]" heading out of the
// repo's CHANGELOG.md, skipping [Unreleased].
func latestReleaseVersion(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "CHANGELOG.md"))
	if err != nil {
		t.Skipf("CHANGELOG.md unreadable: %v", err)
	}
	re := regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+[^\]]*)\]`)
	m := re.FindSubmatch(data)
	if m == nil {
		return ""
	}
	return string(m[1])
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in   string
		ok   bool
		want version
	}{
		{"1.2.3", true, version{1, 2, 3}},
		{"v1.2.3", true, version{1, 2, 3}},
		{"0.1.0-beta.15", true, version{0, 1, 0}},
		{"1.2.3+build.4", true, version{1, 2, 3}},
		{"dev", false, version{}},
		{"", false, version{}},
		{"1.2", true, version{1, 2, 0}}, // semver coerces a missing patch to 0
		{"a.b.c", false, version{}},
	}
	for _, tt := range tests {
		got, ok := parseVersion(tt.in)
		if ok != tt.ok || got != tt.want {
			t.Errorf("parseVersion(%q) = %v, %v; want %v, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}
