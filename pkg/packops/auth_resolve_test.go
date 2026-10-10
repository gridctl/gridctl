package packops

import (
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/pack"
	"github.com/gridctl/gridctl/pkg/skills"
)

func TestResolveSourceAuth_Order(t *testing.T) {
	spec := pack.Source{
		Repo: "https://github.com/acme/netops",
		Auth: &pack.SourceAuth{Method: "token", CredentialRef: "${var:MANIFEST}"},
	}
	caller := skills.AuthConfig{Method: "token", Token: "primary-token", CredentialRef: "${var:PRIMARY}"}
	override := skills.AuthConfig{Method: "ssh-key", SSHKeyPath: "/keys/override"}
	lf := &skills.LockFile{Sources: map[string]skills.LockedSource{
		"team/netops": {CredentialRef: "${var:STORED}"},
	}}
	resolver := func(ref string) (string, error) {
		return "tok-" + ref, nil
	}

	got, err := resolveSourceAuth("team", "netops", spec, &override, caller, lf, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if got.SSHKeyPath != "/keys/override" || got.Token != "" {
		t.Fatalf("override = %+v, want the explicit ssh key and no token", got)
	}

	got, err = resolveSourceAuth("team", "netops", spec, nil, caller, lf, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if got.CredentialRef != "${var:STORED}" || got.Token != "tok-${var:STORED}" {
		t.Fatalf("stored = %+v", got)
	}

	got, err = resolveSourceAuth("team", "netops", spec, nil, caller, &skills.LockFile{}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if got.CredentialRef != "${var:MANIFEST}" || got.Token != "tok-${var:MANIFEST}" {
		t.Fatalf("manifest = %+v", got)
	}
	if got.Token == caller.Token {
		t.Fatal("primary token must not fan out")
	}
}

func TestResolveSourceAuth_SSHKeyFansOutOnlyWhenNothingElseApplies(t *testing.T) {
	spec := pack.Source{Repo: "git@github.com:acme/netops.git"}
	caller := skills.AuthConfig{Method: "ssh-key", SSHKeyPath: "/keys/id"}
	got, err := resolveSourceAuth("team", "netops", spec, nil, caller, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != "ssh-key" || got.SSHKeyPath != "/keys/id" {
		t.Fatalf("ssh fan-out = %+v", got)
	}

	https := pack.Source{Repo: "https://github.com/acme/netops"}
	got, err = resolveSourceAuth("team", "netops", https, nil, caller, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != "" || got.SSHKeyPath != "" {
		t.Fatalf("ssh key must not apply to https, got %+v", got)
	}

	tokenCaller := skills.AuthConfig{Method: "token", Token: "secret", CredentialRef: "${var:PRIMARY}"}
	got, err = resolveSourceAuth("team", "netops", spec, nil, tokenCaller, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != "" || got.CredentialRef != "" {
		t.Fatalf("vault-key must not apply to an external source, got %+v", got)
	}
}

func TestResolveSourceAuth_ManifestSSHKeyMeansAgent(t *testing.T) {
	spec := pack.Source{Repo: "ssh://git@gitlab.example.com/netops.git", Auth: &pack.SourceAuth{Method: "ssh-key", SSHUser: "git"}}
	got, err := resolveSourceAuth("team", "netops", spec, nil, skills.AuthConfig{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != "ssh-agent" || got.SSHUser != "git" || got.SSHKeyPath != "" {
		t.Fatalf("manifest ssh-key = %+v", got)
	}
}

func TestResolveSourceAuth_TokenRequiresRef(t *testing.T) {
	spec := pack.Source{Repo: "https://github.com/acme/netops", Auth: &pack.SourceAuth{Method: "token"}}
	_, err := resolveSourceAuth("team", "netops", spec, nil, skills.AuthConfig{}, nil, func(string) (string, error) {
		return "x", nil
	})
	if err == nil || !strings.Contains(err.Error(), "netops") {
		t.Fatalf("err = %v, want the source name", err)
	}
}
