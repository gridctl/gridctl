package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRender_Golden(t *testing.T) {
	cases := []struct {
		name string
		spec Spec
		file string
	}{
		{name: "systemd stack", spec: stackSpec(PlatformSystemd, false), file: "systemd-stack.service"},
		{name: "systemd pack", spec: packSpec(PlatformSystemd, false), file: "systemd-pack.service"},
		{name: "systemd podman", spec: stackSpec(PlatformSystemd, true), file: "systemd-podman.service"},
		{name: "systemd special", spec: specialSpec(PlatformSystemd), file: "systemd-special.service"},
		{name: "launchd stack", spec: stackSpec(PlatformLaunchd, false), file: "launchd-stack.plist"},
		{name: "launchd pack", spec: packSpec(PlatformLaunchd, false), file: "launchd-pack.plist"},
		{name: "launchd podman", spec: stackSpec(PlatformLaunchd, true), file: "launchd-podman.plist"},
		{name: "launchd special", spec: specialSpec(PlatformLaunchd), file: "launchd-special.plist"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Render(tc.spec)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			again, err := Render(tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			if got != again {
				t.Fatal("render is not deterministic")
			}
			compareGolden(t, tc.file, got)
		})
	}
}

func TestRender_NoCredentialMaterial(t *testing.T) {
	environ := []string{
		"PATH=/usr/bin:/bin",
		"GRIDCTL_VAULT_PASSPHRASE=super-secret",
		"GRIDCTL_SSH_KEY_PASSPHRASE=key-secret",
		"OP_SERVICE_ACCOUNT_TOKEN=op-token",
		"OP_CONNECT_TOKEN=op-connect",
		"AWS_SECRET_ACCESS_KEY=aws-secret",
		"SSH_AUTH_SOCK=/tmp/ssh-abc/agent.12",
		"HOME=/home/u",
	}
	kept, skipped, err := FilterEnvironment(environ, "/home/u")
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 1 || skipped[0] != "/tmp/ssh-abc/agent.12" {
		t.Fatalf("skipped = %v", skipped)
	}
	for _, kv := range kept {
		if kv.Key != "PATH" {
			t.Fatalf("kept unexpected key %s", kv.Key)
		}
	}
	spec, notes, err := BuildSpec(t.Context(), BuildInput{
		Platform:  PlatformSystemd,
		StackName: "netops",
		StackFile: "/home/u/team/stack.yaml",
		Home:      "/home/u",
		Binary:    "/usr/local/bin/gridctl",
		Environ:   environ,
	})
	if err != nil {
		t.Fatal(err)
	}
	text, err := Render(spec)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{
		"super-secret", "key-secret", "op-token", "op-connect", "aws-secret",
		"GRIDCTL_VAULT_PASSPHRASE", "GRIDCTL_SSH_KEY_PASSPHRASE",
		"OP_SERVICE_ACCOUNT_TOKEN", "OP_CONNECT_TOKEN", "AWS_SECRET_ACCESS_KEY",
		"/tmp/ssh-abc/agent.12",
	} {
		if strings.Contains(text, secret) {
			t.Errorf("unit contains %q", secret)
		}
	}
	if len(notes) == 0 || !strings.Contains(notes[0], "/tmp/ssh-abc/agent.12") {
		t.Fatalf("notes = %v", notes)
	}
	if strings.Contains(text, "--bind") || strings.Contains(text, "--insecure-allow-unauthenticated") {
		t.Fatalf("unit forwards a bind flag:\n%s", text)
	}
}

func TestFilterEnvironment_SSHSocketUnderRuntimeDir(t *testing.T) {
	environ := []string{
		"PATH=/usr/bin",
		"XDG_RUNTIME_DIR=/run/user/1000",
		"SSH_AUTH_SOCK=/run/user/1000/ssh-agent.sock",
		"GRIDCTL_EXPERIMENTAL_A2A=1",
		"GRIDCTL_HOME=/should/not/copy",
		"GRIDCTL_RUNTIME=podman",
	}
	kept, skipped, err := FilterEnvironment(environ, "/home/u")
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v", skipped)
	}
	want := []string{"PATH", "GRIDCTL_RUNTIME", "GRIDCTL_EXPERIMENTAL_A2A", "SSH_AUTH_SOCK"}
	if len(kept) != len(want) {
		t.Fatalf("kept = %+v, want keys %v", kept, want)
	}
	for i, key := range want {
		if kept[i].Key != key {
			t.Fatalf("kept[%d] = %s, want %s", i, kept[i].Key, key)
		}
	}
	if kept[3].Value != "/run/user/1000/ssh-agent.sock" {
		t.Fatalf("socket = %s", kept[3].Value)
	}
}

func TestRejectInternalCredentials(t *testing.T) {
	err := rejectInternalCredentials([]KV{{Key: "GRIDCTL_VAULT_PASSPHRASE", Value: "x"}})
	if err == nil {
		t.Fatal("expected internal credential refusal")
	}
}

func TestQuoteSystemd(t *testing.T) {
	got, err := quoteSystemd("/usr/bin")
	if err != nil || got != "/usr/bin" {
		t.Fatalf("plain = %q, %v", got, err)
	}
	got, err = quoteSystemd("/opt/100%/bin")
	if err != nil || got != "/opt/100%%/bin" {
		t.Fatalf("percent = %q, %v", got, err)
	}
	got, err = quoteSystemd("/opt/grid ctl/bin")
	if err != nil || got != `"/opt/grid ctl/bin"` {
		t.Fatalf("space = %q, %v", got, err)
	}
	got, err = quoteSystemd(`/tmp/say"hi`)
	if err != nil || got != `"/tmp/say\"hi"` {
		t.Fatalf("quote = %q, %v", got, err)
	}
	got, err = quoteSystemd(`/tmp/my dir 100%`)
	if err != nil || got != `"/tmp/my dir 100%%"` {
		t.Fatalf("space and percent = %q, %v", got, err)
	}
	got, err = quoteSystemd("/opt/$HOME/bin")
	if err != nil || got != "/opt/$$HOME/bin" {
		t.Fatalf("dollar = %q, %v", got, err)
	}
	if _, err := quoteSystemd("/tmp/has\nnewline"); err == nil {
		t.Fatal("newline was accepted")
	}
}

func stackSpec(platform Platform, podman bool) Spec {
	return Spec{
		Platform:               platform,
		Name:                   "netops",
		StackName:              "netops",
		Label:                  "dev.gridctl.netops",
		Binary:                 "/usr/local/bin/gridctl",
		Argv:                   []string{"/usr/local/bin/gridctl", "apply", "/home/u/team/stack.yaml", "--foreground", "--quiet", "--port", "8180", "--base-port", "9000"},
		Env:                    []KV{{Key: "GRIDCTL_HOME", Value: "/home/u"}, {Key: "PATH", Value: "/usr/bin:/bin"}, {Key: "DOCKER_HOST", Value: "unix:///run/docker.sock"}},
		Home:                   "/home/u",
		LogPath:                "/home/u/.gridctl/logs/netops.log",
		UnitPath:               "/home/u/.config/systemd/user/gridctl-netops.service",
		PodmanSocketDependency: podman,
		StackFile:              "/home/u/team/stack.yaml",
		Port:                   8180,
		BasePort:               9000,
	}
}

func packSpec(platform Platform, podman bool) Spec {
	spec := stackSpec(platform, podman)
	spec.Name = "demo"
	spec.StackName = "demo"
	spec.Label = "dev.gridctl.demo"
	spec.Pack = "demo"
	spec.StackFile = "/home/u/.gridctl/packs/demo/abc123/stack.yaml"
	spec.LogPath = "/home/u/.gridctl/logs/demo.log"
	spec.Argv[2] = spec.StackFile
	return spec
}

func specialSpec(platform Platform) Spec {
	spec := stackSpec(platform, false)
	spec.Name = "my-stack"
	spec.StackName = "my stack"
	spec.Label = "dev.gridctl.my-stack"
	spec.Binary = "/opt/grid ctl/bin/gridctl"
	spec.StackFile = "/home/u/my stacks/100%/stack.yaml"
	spec.LogPath = "/home/u/.gridctl/logs/my stack.log"
	spec.Env = []KV{
		{Key: "GRIDCTL_HOME", Value: "/home/u"},
		{Key: "PATH", Value: "/usr/bin:/opt/100%/bin"},
	}
	spec.Argv = []string{spec.Binary, "apply", spec.StackFile, "--foreground", "--quiet", "--port", "8180", "--base-port", "9000"}
	return spec
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("GRIDCTL_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (regenerate with GRIDCTL_UPDATE_GOLDEN=1): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden %s mismatch\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}
