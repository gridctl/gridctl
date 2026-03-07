package runtime

import (
	"testing"
)

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"4.7.0", "4.7.0", 0},
		{"4.8.0", "4.7.0", 1},
		{"4.6.0", "4.7.0", -1},
		{"5.0.0", "4.7.0", 1},
		{"4.7.1", "4.7.0", 1},
		{"v4.7.0", "4.7.0", 0},
		{"4.7", "4.7.0", 0},
		{"", "4.7.0", -1},
	}
	for _, tt := range tests {
		got := compareSemver(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("compareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input string
		want  [3]int
	}{
		{"4.7.2", [3]int{4, 7, 2}},
		{"v5.0.1", [3]int{5, 0, 1}},
		{"4.7", [3]int{4, 7, 0}},
		{"27.3.1", [3]int{27, 3, 1}},
		{"", [3]int{0, 0, 0}},
	}
	for _, tt := range tests {
		got := parseSemver(tt.input)
		if got != tt.want {
			t.Errorf("parseSemver(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestResolveHostAlias(t *testing.T) {
	tests := []struct {
		rt      RuntimeType
		version string
		want    string
	}{
		{RuntimeDocker, "27.3.1", "host.docker.internal"},
		{RuntimePodman, "5.0.0", "host.containers.internal"},
		{RuntimePodman, "4.7.0", "host.containers.internal"},
		{RuntimePodman, "4.6.0", "host.docker.internal"},
		{RuntimePodman, "4.4.0", "host.docker.internal"},
		{RuntimePodman, "", "host.docker.internal"},
	}
	for _, tt := range tests {
		got := resolveHostAlias(tt.rt, tt.version)
		if got != tt.want {
			t.Errorf("resolveHostAlias(%s, %q) = %q, want %q", tt.rt, tt.version, got, tt.want)
		}
	}
}

func TestApplyVolumeLabels(t *testing.T) {
	// No change for Docker
	dockerInfo := &RuntimeInfo{Type: RuntimeDocker, SELinux: true}
	vols := []string{"/host:/container", "/a:/b:rw"}
	got := ApplyVolumeLabels(vols, dockerInfo)
	if len(got) != 2 || got[0] != "/host:/container" || got[1] != "/a:/b:rw" {
		t.Errorf("Docker should not modify volumes, got %v", got)
	}

	// No change for Podman without SELinux
	podmanNoSE := &RuntimeInfo{Type: RuntimePodman, SELinux: false}
	got = ApplyVolumeLabels(vols, podmanNoSE)
	if got[0] != "/host:/container" {
		t.Errorf("Podman without SELinux should not modify volumes, got %v", got)
	}

	// Add :Z for Podman + SELinux
	podmanSE := &RuntimeInfo{Type: RuntimePodman, SELinux: true}
	got = ApplyVolumeLabels(vols, podmanSE)
	if got[0] != "/host:/container:Z" {
		t.Errorf("expected /host:/container:Z, got %s", got[0])
	}
	if got[1] != "/a:/b:rw,Z" {
		t.Errorf("expected /a:/b:rw,Z, got %s", got[1])
	}

	// Nil info should not modify
	got = ApplyVolumeLabels(vols, nil)
	if got[0] != "/host:/container" {
		t.Errorf("nil info should not modify volumes, got %v", got)
	}
}

func TestRuntimeInfo_IsRootless(t *testing.T) {
	tests := []struct {
		rt     RuntimeType
		socket string
		want   bool
	}{
		{RuntimeDocker, "/var/run/docker.sock", false},
		{RuntimePodman, "/run/podman/podman.sock", false},
		{RuntimePodman, "/run/user/1000/podman/podman.sock", true},
		{RuntimePodman, "/tmp/podman.sock", true},
	}
	for _, tt := range tests {
		info := &RuntimeInfo{Type: tt.rt, SocketPath: tt.socket}
		if got := info.IsRootless(); got != tt.want {
			t.Errorf("IsRootless() for %s at %s = %v, want %v", tt.rt, tt.socket, got, tt.want)
		}
	}
}

func TestRuntimeInfo_DisplayName(t *testing.T) {
	docker := &RuntimeInfo{Type: RuntimeDocker}
	if docker.DisplayName() != "docker" {
		t.Errorf("expected 'docker', got %q", docker.DisplayName())
	}

	podman := &RuntimeInfo{Type: RuntimePodman}
	if podman.DisplayName() != "podman (experimental)" {
		t.Errorf("expected 'podman (experimental)', got %q", podman.DisplayName())
	}
}

func TestRuntimeInfo_DockerHost(t *testing.T) {
	info := &RuntimeInfo{SocketPath: "/run/podman/podman.sock"}
	if info.DockerHost() != "unix:///run/podman/podman.sock" {
		t.Errorf("expected unix:///run/podman/podman.sock, got %q", info.DockerHost())
	}
}

func TestExtractSocketPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"unix:///var/run/docker.sock", "/var/run/docker.sock"},
		{"unix:///run/podman/podman.sock", "/run/podman/podman.sock"},
		{"tcp://localhost:2375", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractSocketPath(tt.input)
		if got != tt.want {
			t.Errorf("extractSocketPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
