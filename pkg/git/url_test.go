package git

import "testing"

func TestHTTPSEquivalent(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"scp syntax with .git", "git@github.com:acme/pack.git", "https://github.com/acme/pack", true},
		{"scp syntax without .git", "git@github.com:acme/pack", "https://github.com/acme/pack", true},
		{"scp syntax nested path", "git@gitlab.com:acme/team/pack.git", "https://gitlab.com/acme/team/pack", true},
		{"scp syntax non-git user", "deploy@git.internal:acme/pack.git", "https://git.internal/acme/pack", true},
		{"ssh scheme", "ssh://git@github.com/acme/pack.git", "https://github.com/acme/pack", true},
		{"ssh scheme with port dropped", "ssh://git@git.internal:2222/acme/pack.git", "https://git.internal/acme/pack", true},
		{"https input is not ssh", "https://github.com/acme/pack", "", false},
		{"local path", "/srv/packs/acme", "", false},
		{"empty", "", "", false},
		{"scp syntax with no path", "git@github.com:", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := HTTPSEquivalent(c.in)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v (got %q)", ok, c.ok, got)
			}
			if got != c.want {
				t.Errorf("HTTPSEquivalent(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
