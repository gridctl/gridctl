package depcheck

import (
	"strings"
	"testing"
)

func TestClassifyImage_Table(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	tests := []struct {
		name   string
		input  string
		status Status
		reason string
	}{
		{name: "untagged", input: "nginx", status: StatusMutable, reason: "mutable-tag"},
		{name: "latest tag", input: "nginx:latest", status: StatusMutable, reason: "mutable-tag"},
		{name: "exact looking version tag", input: "nginx:1.21.0", status: StatusMutable, reason: "mutable-tag"},
		{name: "registry port tag", input: "localhost:5000/foo:1.2", status: StatusMutable, reason: "mutable-tag"},
		{name: "digest only", input: digest, status: StatusPinned, reason: "digest"},
		{name: "name digest", input: "nginx@" + digest, status: StatusPinned, reason: "digest"},
		{name: "tag plus digest", input: "nginx:1.21.0@" + digest, status: StatusPinned, reason: "digest"},
		{name: "registry port digest", input: "localhost:5000/foo@" + digest, status: StatusPinned, reason: "digest"},
		{name: "ghcr tag plus digest", input: "ghcr.io/github/github-mcp-server:v1.12.1@" + digest, status: StatusPinned, reason: "digest"},
		{name: "invalid digest hex", input: "nginx@sha256:not-a-digest", status: StatusNotAssessed, reason: "invalid-digest"},
		{name: "truncated digest", input: "nginx@sha256:abcd", status: StatusNotAssessed, reason: "invalid-digest"},
		{name: "empty digest algorithm", input: "nginx@sha256:", status: StatusNotAssessed, reason: "invalid-digest"},
		{name: "bare truncated digest", input: "sha256:abcd", status: StatusNotAssessed, reason: "invalid-digest"},
		{name: "registry port invalid digest", input: "localhost:5000/foo@sha256:abcd", status: StatusNotAssessed, reason: "invalid-digest"},
		{name: "variable var", input: "${var:IMAGE}", status: StatusNotAssessed, reason: "variable"},
		{name: "variable env", input: "$IMAGE", status: StatusNotAssessed, reason: "variable"},
		{name: "variable mixed", input: "nginx:${TAG}", status: StatusNotAssessed, reason: "variable"},
		{name: "empty", input: "  ", status: StatusNotAssessed, reason: "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyImage(tt.input)
			if got.Kind != KindImage {
				t.Errorf("Kind = %q, want %q", got.Kind, KindImage)
			}
			if got.Status != tt.status {
				t.Errorf("Status = %q, want %q (reason %q)", got.Status, tt.status, got.Reason)
			}
			if got.Reason != tt.reason {
				t.Errorf("Reason = %q, want %q", got.Reason, tt.reason)
			}
			if strings.Contains(got.Reason, tt.input) && tt.input != "" && strings.TrimSpace(tt.input) != "" {
				t.Errorf("reason leaked selector %q", tt.input)
			}
		})
	}
}

func TestClassifyImage_DoesNotClaimPublisherTrust(t *testing.T) {
	digest := "sha256:" + strings.Repeat("b", 64)
	got := ClassifyImage("nginx@" + digest)
	if got.Status != StatusPinned {
		t.Fatalf("Status = %q", got.Status)
	}
	if got.Reason != "digest" {
		t.Fatalf("Reason = %q", got.Reason)
	}
}

func TestClassifyCommand_Npx(t *testing.T) {
	tests := []struct {
		name   string
		argv   []string
		kind   Kind
		status Status
		reason string
	}{
		{name: "missing version", argv: []string{"npx", "-y", "chrome-devtools-mcp"}, kind: KindNPM, status: StatusMutable, reason: "missing-version"},
		{name: "latest tag", argv: []string{"npx", "-y", "chrome-devtools-mcp@latest"}, kind: KindNPM, status: StatusMutable, reason: "floating-range"},
		{name: "caret range", argv: []string{"npx", "pkg@^1.2.3"}, kind: KindNPM, status: StatusMutable, reason: "floating-range"},
		{name: "exact version", argv: []string{"npx", "-y", "chrome-devtools-mcp@1.9.0"}, kind: KindNPM, status: StatusPinned, reason: "exact-version"},
		{name: "scoped missing version", argv: []string{"npx", "-y", "@upstash/context7-mcp"}, kind: KindNPM, status: StatusMutable, reason: "missing-version"},
		{name: "scoped exact", argv: []string{"npx", "-y", "@playwright/mcp@0.0.80"}, kind: KindNPM, status: StatusPinned, reason: "exact-version"},
		{name: "scoped latest", argv: []string{"npx", "@playwright/mcp@latest"}, kind: KindNPM, status: StatusMutable, reason: "floating-range"},
		{name: "package flag exact", argv: []string{"npx", "-p", "pkg@1.2.3", "pkg"}, kind: KindNPM, status: StatusPinned, reason: "exact-version"},
		{name: "package equals flag", argv: []string{"npx", "--package=pkg@1.2.3", "run"}, kind: KindNPM, status: StatusPinned, reason: "exact-version"},
		{name: "command args after spec ignored", argv: []string{"npx", "-y", "pkg@1.2.3", "--token", "s3cret-value"}, kind: KindNPM, status: StatusPinned, reason: "exact-version"},
		{name: "call option", argv: []string{"npx", "-c", "echo hi"}, kind: KindNPM, status: StatusNotAssessed, reason: "unsupported-option"},
		{name: "unknown option", argv: []string{"npx", "--prefix", "/tmp", "pkg@1.0.0"}, kind: KindNPM, status: StatusNotAssessed, reason: "unsupported-option"},
		{name: "local path", argv: []string{"npx", "./local-pkg"}, kind: KindNPM, status: StatusNotAssessed, reason: "local-path"},
		{name: "absolute path", argv: []string{"npx", "/opt/pkg"}, kind: KindNPM, status: StatusNotAssessed, reason: "local-path"},
		{name: "variable spec", argv: []string{"npx", "-y", "${var:PKG}"}, kind: KindNPM, status: StatusNotAssessed, reason: "variable"},
		{name: "variable launcher", argv: []string{"${var:LAUNCHER}", "pkg@1.0.0"}, kind: KindNone, status: StatusNotAssessed, reason: "variable"},
		{name: "path to npx", argv: []string{"/usr/bin/npx", "pkg@1.2.3"}, kind: KindNPM, status: StatusPinned, reason: "exact-version"},
		{name: "empty npx", argv: []string{"npx"}, kind: KindNPM, status: StatusNotAssessed, reason: "unknown-form"},
		{name: "double dash before spec", argv: []string{"npx", "-y", "--", "pkg@1.2.3"}, kind: KindNPM, status: StatusNotAssessed, reason: "unknown-form"},
		{name: "double dash after spec", argv: []string{"npx", "-y", "pkg@1.2.3", "--", "--token", "s3cret-value"}, kind: KindNPM, status: StatusPinned, reason: "exact-version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyCommand(tt.argv)
			if got.Kind != tt.kind {
				t.Errorf("Kind = %q, want %q", got.Kind, tt.kind)
			}
			if got.Status != tt.status {
				t.Errorf("Status = %q, want %q (reason %q)", got.Status, tt.status, got.Reason)
			}
			if got.Reason != tt.reason {
				t.Errorf("Reason = %q, want %q", got.Reason, tt.reason)
			}
			if strings.Contains(got.Reason, "s3cret-value") {
				t.Errorf("result leaked command material: %+v", got)
			}
		})
	}
}

func TestClassifyCommand_Uvx(t *testing.T) {
	tests := []struct {
		name   string
		argv   []string
		status Status
		reason string
	}{
		{name: "missing version", argv: []string{"uvx", "httpie"}, status: StatusMutable, reason: "missing-version"},
		{name: "exact equality", argv: []string{"uvx", "httpie==1.2.3"}, status: StatusPinned, reason: "exact-version"},
		{name: "from exact", argv: []string{"uvx", "--from", "mcp-server-fetch==2026.8.18", "mcp-server-fetch"}, status: StatusPinned, reason: "exact-version"},
		{name: "from extras exact", argv: []string{"uvx", "--from", "huggingface-hub[cli,torch]==0.1.0", "hf"}, status: StatusPinned, reason: "exact-version"},
		{name: "from equals flag", argv: []string{"uvx", "--from=pkg==1.0.0", "pkg"}, status: StatusPinned, reason: "exact-version"},
		{name: "range", argv: []string{"uvx", "httpie>=1.0"}, status: StatusMutable, reason: "floating-range"},
		{name: "compatible release", argv: []string{"uvx", "httpie~=1.2"}, status: StatusMutable, reason: "floating-range"},
		{name: "python option skipped", argv: []string{"uvx", "--python", "3.12", "httpie==1.2.3"}, status: StatusPinned, reason: "exact-version"},
		{name: "with extra skipped", argv: []string{"uvx", "--with", "httpx", "httpie==1.2.3"}, status: StatusPinned, reason: "exact-version"},
		{name: "unknown option", argv: []string{"uvx", "--index-url", "https://example.invalid", "httpie==1.2.3"}, status: StatusNotAssessed, reason: "unsupported-option"},
		{name: "git from", argv: []string{"uvx", "--from", "git+https://github.com/httpie/cli", "httpie"}, status: StatusNotAssessed, reason: "local-path"},
		{name: "local path", argv: []string{"uvx", "./tools/pkg"}, status: StatusNotAssessed, reason: "local-path"},
		{name: "variable from", argv: []string{"uvx", "--from", "${var:PKG}", "tool"}, status: StatusNotAssessed, reason: "variable"},
		{name: "command args after package", argv: []string{"uvx", "httpie==1.2.3", "--auth", "s3cret-value"}, status: StatusPinned, reason: "exact-version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyCommand(tt.argv)
			if got.Kind != KindPyPI {
				t.Errorf("Kind = %q, want %q", got.Kind, KindPyPI)
			}
			if got.Status != tt.status {
				t.Errorf("Status = %q, want %q (reason %q)", got.Status, tt.status, got.Reason)
			}
			if got.Reason != tt.reason {
				t.Errorf("Reason = %q, want %q", got.Reason, tt.reason)
			}
			if strings.Contains(got.Reason, "s3cret-value") {
				t.Errorf("reason leaked argument: %q", got.Reason)
			}
		})
	}
}

func TestClassifyCommand_UnknownWrappersAreNotAssessed(t *testing.T) {
	tests := [][]string{
		{"npm", "exec", "pkg@1.2.3"},
		{"yarn", "dlx", "pkg@1.2.3"},
		{"python", "-m", "mcp_server"},
		{"bash", "-c", "npx pkg@1.2.3"},
		{"sh", "-c", "while true; do sleep 3600; done"},
		{"../_mock-servers/local-stdio-server/mock-stdio-server"},
	}
	for _, argv := range tests {
		got := ClassifyCommand(argv)
		if got.Status != StatusNotAssessed {
			t.Errorf("%v Status = %q, want not-assessed", argv, got.Status)
		}
		if got.Status == StatusPinned {
			t.Errorf("%v must not receive a complete-assessment claim", argv)
		}
	}
}

func TestIsPackageLauncher(t *testing.T) {
	if !IsPackageLauncher([]string{"npx", "pkg"}) {
		t.Fatal("npx")
	}
	if !IsPackageLauncher([]string{"/usr/bin/uvx", "pkg"}) {
		t.Fatal("uvx path")
	}
	if !IsPackageLauncher([]string{"npm", "exec", "pkg"}) {
		t.Fatal("npm")
	}
	if IsPackageLauncher([]string{"sh", "-c", "true"}) {
		t.Fatal("sh is not a package launcher")
	}
	if IsPackageLauncher(nil) {
		t.Fatal("empty")
	}
}

func TestStatusNotAssessed_PolicyMustNotAccept(t *testing.T) {
	got := ClassifyCommand([]string{"npm", "exec", "pkg@1.2.3"})
	if got.Status != StatusNotAssessed {
		t.Fatalf("Status = %q, want %s (policy callers must treat this as unknown and must not accept it)", got.Status, StatusNotAssessed)
	}
}

func TestClassifier_NoNetworkAndNoRewrite(t *testing.T) {
	input := "nginx:1.21.0"
	_ = ClassifyImage(input)
	if input != "nginx:1.21.0" {
		t.Fatal("ClassifyImage rewrote the caller string")
	}
	argv := []string{"npx", "-y", "pkg@1.2.3"}
	_ = ClassifyCommand(argv)
	if argv[2] != "pkg@1.2.3" {
		t.Fatal("ClassifyCommand rewrote argv")
	}
}
