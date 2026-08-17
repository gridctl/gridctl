package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	gitpkg "github.com/gridctl/gridctl/pkg/git"
)

func TestValidateSkillAuthFlags(t *testing.T) {
	cases := []struct {
		name       string
		token      string
		vaultKey   string
		tokenStdin bool
		wantErr    string
	}{
		{name: "all empty"},
		{name: "literal token only", token: "ghp_x"},
		{name: "vault key only", vaultKey: "GIT_TOKEN"},
		{name: "stdin only", tokenStdin: true},
		{name: "dash sentinel only", token: "-"},
		{
			name: "literal token and vault key", token: "ghp_x", vaultKey: "GIT_TOKEN",
			wantErr: "--auth-token and --vault-key are mutually exclusive",
		},
		{
			name: "stdin and vault key", tokenStdin: true, vaultKey: "GIT_TOKEN",
			wantErr: "--auth-token-stdin and --vault-key are mutually exclusive",
		},
		{
			// The sentinel is the stdin form, so it collides with vault-key
			// the same way the flag does.
			name: "dash sentinel and vault key", token: "-", vaultKey: "GIT_TOKEN",
			wantErr: "--auth-token-stdin and --vault-key are mutually exclusive",
		},
		{
			name: "literal token and stdin", token: "ghp_x", tokenStdin: true,
			wantErr: "--auth-token and --auth-token-stdin are mutually exclusive",
		},
		{
			// Redundant but not contradictory: both name the same source.
			name: "dash sentinel and stdin flag", token: "-", tokenStdin: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			token, vaultKey, stdin := c.token, c.vaultKey, c.tokenStdin
			err := validateSkillAuthFlags(&token, &vaultKey, &stdin)(nil, nil)
			switch {
			case c.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case c.wantErr != "" && err == nil:
				t.Fatalf("expected error %q, got nil", c.wantErr)
			case c.wantErr != "" && err.Error() != c.wantErr:
				t.Errorf("error = %q, want %q", err.Error(), c.wantErr)
			}
		})
	}
}

func TestBuildAuthConfigFromFlags(t *testing.T) {
	cases := []struct {
		name       string
		token      string
		tokenStdin bool
		vaultKey   string
		sshKey     string
		stdin      string
		wantMethod string
		wantToken  string
		wantWarn   bool
	}{
		{name: "no flags yields ambient", wantMethod: ""},
		{
			name: "literal token warns", token: "ghp_literal",
			wantMethod: "token", wantToken: "ghp_literal", wantWarn: true,
		},
		{
			name: "stdin token does not warn", tokenStdin: true, stdin: "ghp_piped\n",
			wantMethod: "token", wantToken: "ghp_piped",
		},
		{
			name: "dash sentinel reads stdin", token: "-", stdin: "ghp_dash\n",
			wantMethod: "token", wantToken: "ghp_dash",
		},
		{
			// Tokens copied out of a provider UI carry trailing whitespace,
			// which otherwise fails auth in a way that looks like a bad token.
			name: "stdin token is trimmed", tokenStdin: true, stdin: "  ghp_spaced \n\n",
			wantMethod: "token", wantToken: "ghp_spaced",
		},
		{
			name: "ssh key wins over token", sshKey: "/keys/id_ed25519", token: "ghp_x",
			wantMethod: "ssh-key",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var stderr bytes.Buffer
			cfg, err := buildAuthConfigFromFlags(&stderr, strings.NewReader(c.stdin),
				c.token, c.tokenStdin, c.vaultKey, c.sshKey)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.Method != c.wantMethod {
				t.Errorf("Method = %q, want %q", cfg.Method, c.wantMethod)
			}
			if c.wantToken != "" && cfg.Token != c.wantToken {
				t.Errorf("Token = %q, want %q", cfg.Token, c.wantToken)
			}
			// Only --vault-key produces a persistable reference; a literal or
			// piped token must never leave one behind.
			if c.vaultKey == "" && cfg.CredentialRef != "" {
				t.Errorf("CredentialRef = %q, want empty without --vault-key", cfg.CredentialRef)
			}
			warned := strings.Contains(stderr.String(), "warning:")
			if warned != c.wantWarn {
				t.Errorf("warned = %v, want %v (stderr: %q)", warned, c.wantWarn, stderr.String())
			}
			if c.wantWarn && !strings.Contains(stderr.String(), "shell history") {
				t.Errorf("warning should name the exposure, got %q", stderr.String())
			}
			// The warning must never echo the secret it is warning about.
			if c.token != "" && strings.Contains(stderr.String(), c.token) {
				t.Errorf("stderr echoed the token: %q", stderr.String())
			}
		})
	}
}

func TestBuildAuthConfigFromFlags_EmptyStdinIsAnError(t *testing.T) {
	var stderr bytes.Buffer
	_, err := buildAuthConfigFromFlags(&stderr, strings.NewReader("   \n"), "", true, "", "")
	if err == nil {
		t.Fatal("expected an error when stdin carries no token")
	}
	if !strings.Contains(err.Error(), "no token read from stdin") {
		t.Errorf("error = %q, want it to name the empty stdin", err.Error())
	}
}

func TestPrintSkillAuthHint(t *testing.T) {
	cases := []struct {
		name     string
		repo     string
		err      error
		want     bool
		contains []string
	}{
		{
			name: "auth required puts the safer option first",
			err:  fmt.Errorf("%w: x", gitpkg.ErrAuthRequired),
			want: true,
			// Ordering is the assertion: --vault-key must be recommended
			// ahead of the flag that leaks into shell history.
			contains: []string{"--vault-key (recommended) or --auth-token"},
		},
		{
			name:     "auth failed",
			err:      fmt.Errorf("%w: x", gitpkg.ErrAuthFailed),
			want:     true,
			contains: []string{"repo-read access"},
		},
		{
			name:     "ssh agent missing with ssh repo offers the https URL",
			repo:     "git@github.com:acme/pack.git",
			err:      fmt.Errorf("%w: x", gitpkg.ErrSSHAgentMissing),
			want:     true,
			contains: []string{"https://github.com/acme/pack", "restart the daemon", "--ssh-key"},
		},
		{
			name:     "ssh agent missing without a usable repo still names the remedies",
			err:      fmt.Errorf("%w: x", gitpkg.ErrSSHAgentMissing),
			want:     true,
			contains: []string{"--vault-key (recommended)", "--ssh-key"},
		},
		{
			name:     "host key mismatch",
			err:      fmt.Errorf("%w: x", gitpkg.ErrHostKeyMismatch),
			want:     true,
			contains: []string{"known_hosts"},
		},
		{
			name: "unclassified error gets no hint",
			err:  errors.New("disk on fire"),
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var stderr bytes.Buffer
			got := printSkillAuthHint(&stderr, c.repo, c.err)
			if got != c.want {
				t.Fatalf("printSkillAuthHint = %v, want %v", got, c.want)
			}
			for _, want := range c.contains {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("hint should mention %q, got %q", want, stderr.String())
				}
			}
			if !c.want && stderr.Len() != 0 {
				t.Errorf("expected no output, got %q", stderr.String())
			}
		})
	}
}

func TestReadTokenFromStdin_Bounded(t *testing.T) {
	// A pipe that never ends must not be read without limit.
	huge := strings.Repeat("a", 32*1024)
	token, err := readTokenFromStdin(strings.NewReader(huge))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(token) > 8192 {
		t.Errorf("token length = %d, want it capped at 8192", len(token))
	}
}
