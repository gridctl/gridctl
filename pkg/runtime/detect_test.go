package runtime

import "testing"

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"4.7.0", "4.7.0", 0},
		{"4.6.0", "4.7.0", -1},
		{"4.8.0", "4.7.0", 1},
		{"5.0.0", "4.7.0", 1},
		{"4.7.1", "4.7.0", 1},
		{"4.4.0", "4.7.0", -1},
		{"v5.8.0", "4.7.0", 1},
		{"26.1.3", "4.7.0", 1},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			got := compareSemver(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("compareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input string
		want  [3]int
	}{
		{"4.7.0", [3]int{4, 7, 0}},
		{"v5.8.1", [3]int{5, 8, 1}},
		{"26.1.3", [3]int{26, 1, 3}},
		{"4.4", [3]int{4, 4, 0}},
		{"5", [3]int{5, 0, 0}},
		{"", [3]int{0, 0, 0}},
		{"notaversion", [3]int{0, 0, 0}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseSemver(tt.input)
			if got != tt.want {
				t.Errorf("parseSemver(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveHostAlias(t *testing.T) {
	tests := []struct {
		name string
		info RuntimeInfo
		want string
	}{
		{
			name: "docker",
			info: RuntimeInfo{Type: RuntimeDocker, Version: "26.1.3"},
			want: "host.docker.internal:host-gateway",
		},
		{
			name: "podman 4.7+",
			info: RuntimeInfo{Type: RuntimePodman, Version: "4.7.0"},
			want: "host.containers.internal:host-gateway",
		},
		{
			name: "podman 5.x",
			info: RuntimeInfo{Type: RuntimePodman, Version: "5.8.0"},
			want: "host.containers.internal:host-gateway",
		},
		{
			name: "podman 4.4 (old)",
			info: RuntimeInfo{Type: RuntimePodman, Version: "4.4.0"},
			want: "host.docker.internal:host-gateway",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveHostAlias(&tt.info)
			if got != tt.want {
				t.Errorf("resolveHostAlias() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRuntimeInfo_DisplayName(t *testing.T) {
	tests := []struct {
		info RuntimeInfo
		want string
	}{
		{RuntimeInfo{Type: RuntimeDocker}, "docker"},
		{RuntimeInfo{Type: RuntimePodman}, "podman (experimental)"},
	}

	for _, tt := range tests {
		t.Run(string(tt.info.Type), func(t *testing.T) {
			got := tt.info.DisplayName()
			if got != tt.want {
				t.Errorf("DisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRuntimeInfo_CLIName(t *testing.T) {
	if (&RuntimeInfo{Type: RuntimeDocker}).CLIName() != "docker" {
		t.Error("expected docker CLI name")
	}
	if (&RuntimeInfo{Type: RuntimePodman}).CLIName() != "podman" {
		t.Error("expected podman CLI name")
	}
}

func TestRuntimeInfo_DockerHost(t *testing.T) {
	tests := []struct {
		name string
		info RuntimeInfo
		want string
	}{
		{
			name: "docker default socket",
			info: RuntimeInfo{Type: RuntimeDocker, SocketPath: "/var/run/docker.sock"},
			want: "",
		},
		{
			name: "podman socket",
			info: RuntimeInfo{Type: RuntimePodman, SocketPath: "/run/podman/podman.sock"},
			want: "unix:///run/podman/podman.sock",
		},
		{
			name: "docker custom socket",
			info: RuntimeInfo{Type: RuntimeDocker, SocketPath: "/custom/docker.sock"},
			want: "unix:///custom/docker.sock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.info.DockerHost()
			if got != tt.want {
				t.Errorf("DockerHost() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRuntimeInfo_HostAliasHostname(t *testing.T) {
	info := RuntimeInfo{HostAlias: "host.docker.internal:host-gateway"}
	if info.HostAliasHostname() != "host.docker.internal" {
		t.Errorf("got %q, want host.docker.internal", info.HostAliasHostname())
	}

	info = RuntimeInfo{HostAlias: "host.containers.internal:host-gateway"}
	if info.HostAliasHostname() != "host.containers.internal" {
		t.Errorf("got %q, want host.containers.internal", info.HostAliasHostname())
	}
}

func TestApplyVolumeLabels(t *testing.T) {
	tests := []struct {
		name    string
		info    RuntimeInfo
		volumes []string
		want    []string
	}{
		{
			name:    "docker - no change",
			info:    RuntimeInfo{Type: RuntimeDocker, SELinux: true},
			volumes: []string{"/host:/container"},
			want:    []string{"/host:/container"},
		},
		{
			name:    "podman no selinux - no change",
			info:    RuntimeInfo{Type: RuntimePodman, SELinux: false},
			volumes: []string{"/host:/container"},
			want:    []string{"/host:/container"},
		},
		{
			name:    "podman selinux - append Z",
			info:    RuntimeInfo{Type: RuntimePodman, SELinux: true},
			volumes: []string{"/host:/container"},
			want:    []string{"/host:/container:Z"},
		},
		{
			name:    "podman selinux - with mode",
			info:    RuntimeInfo{Type: RuntimePodman, SELinux: true},
			volumes: []string{"/host:/container:ro"},
			want:    []string{"/host:/container:ro,Z"},
		},
		{
			name:    "podman selinux - already has Z",
			info:    RuntimeInfo{Type: RuntimePodman, SELinux: true},
			volumes: []string{"/host:/container:Z"},
			want:    []string{"/host:/container:Z"},
		},
		{
			name:    "podman selinux - already has z lowercase",
			info:    RuntimeInfo{Type: RuntimePodman, SELinux: true},
			volumes: []string{"/host:/container:z"},
			want:    []string{"/host:/container:z"},
		},
		{
			name:    "empty volumes",
			info:    RuntimeInfo{Type: RuntimePodman, SELinux: true},
			volumes: []string{},
			want:    []string{},
		},
		{
			name:    "nil volumes",
			info:    RuntimeInfo{Type: RuntimePodman, SELinux: true},
			volumes: nil,
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.info.ApplyVolumeLabels(tt.volumes)
			if len(got) != len(tt.want) {
				t.Fatalf("len(ApplyVolumeLabels()) = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ApplyVolumeLabels()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDetectRootlessPodman(t *testing.T) {
	tests := []struct {
		socket string
		want   bool
	}{
		{"/run/podman/podman.sock", false},
		{"/run/user/1000/podman/podman.sock", true},
		{"/tmp/podman.sock", true},
	}

	for _, tt := range tests {
		t.Run(tt.socket, func(t *testing.T) {
			got := detectRootlessPodman(tt.socket)
			if got != tt.want {
				t.Errorf("detectRootlessPodman(%q) = %v, want %v", tt.socket, got, tt.want)
			}
		})
	}
}
