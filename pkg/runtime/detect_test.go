package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
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
	if podman.DisplayName() != "podman" {
		t.Errorf("expected 'podman', got %q", podman.DisplayName())
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
		{"unix:///Users/foo/.orbstack/run/docker.sock", "/Users/foo/.orbstack/run/docker.sock"},
		{"tcp://localhost:2375", ""},
		{"ssh://user@host", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractSocketPath(tt.input)
		if got != tt.want {
			t.Errorf("extractSocketPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// writeDockerContext writes a fake Docker CLI config + per-context meta file
// under dockerCfgDir so resolveDockerContext can resolve a unix:// endpoint.
func writeDockerContext(t *testing.T, dockerCfgDir, contextName, host string) {
	t.Helper()
	if err := os.MkdirAll(dockerCfgDir, 0o755); err != nil {
		t.Fatalf("mkdir docker cfg dir: %v", err)
	}
	cfg := []byte(`{"currentContext":"` + contextName + `"}`)
	if err := os.WriteFile(filepath.Join(dockerCfgDir, "config.json"), cfg, 0o600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}
	sum := sha256.Sum256([]byte(contextName))
	metaDir := filepath.Join(dockerCfgDir, "contexts", "meta", hex.EncodeToString(sum[:]))
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("mkdir meta dir: %v", err)
	}
	meta := []byte(`{"Endpoints":{"docker":{"Host":"` + host + `"}}}`)
	if err := os.WriteFile(filepath.Join(metaDir, "meta.json"), meta, 0o600); err != nil {
		t.Fatalf("write meta.json: %v", err)
	}
}

// shortTempDir returns a tempdir under /tmp with a short prefix. macOS limits
// unix socket paths to 104 bytes, and t.TempDir() includes the test name —
// long names easily blow the budget.
func shortTempDir(t *testing.T) string {
	t.Helper()
	base := "/tmp"
	if _, err := os.Stat(base); err != nil {
		base = os.TempDir()
	}
	dir, err := os.MkdirTemp(base, "gctl")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// serveUnixSocket binds a unix listener at path and serves /_ping (200) plus
// a stub /version response so probeSocket and queryVersion both succeed.
func serveUnixSocket(t *testing.T, path string) {
	t.Helper()
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen unix %q: %v (path may exceed OS socket length limit)", path, err)
	}
	t.Cleanup(func() { _ = l.Close() })
	mux := http.NewServeMux()
	mux.HandleFunc("/_ping", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Version":"27.0.0","Components":[{"Name":"Engine"}]}`))
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second}
	t.Cleanup(func() { _ = srv.Close() })
	go func() { _ = srv.Serve(l) }()
}

func TestProbeSocket_DanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "s")
	if err := os.Symlink(filepath.Join(dir, "missing"), link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if probeSocket(link) {
		t.Fatalf("probeSocket on dangling symlink should be false")
	}
}

func TestProbeSocket_RealSocket(t *testing.T) {
	sock := filepath.Join(shortTempDir(t), "s")
	serveUnixSocket(t, sock)
	if !probeSocket(sock) {
		t.Fatalf("probeSocket on live unix socket should be true")
	}
}

func TestProbeSocket_NonSocket(t *testing.T) {
	f := filepath.Join(t.TempDir(), "s")
	if err := os.WriteFile(f, []byte("not a socket"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if probeSocket(f) {
		t.Fatalf("probeSocket on regular file should be false")
	}
}

func TestResolveDockerContext_MissingConfig(t *testing.T) {
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	got, err := resolveDockerContext()
	if err != nil || got != "" {
		t.Fatalf("missing config: got (%q, %v), want (\"\", nil)", got, err)
	}
}

func TestResolveDockerContext_EmptyCurrentContext(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := resolveDockerContext()
	if err != nil || got != "" {
		t.Fatalf("empty currentContext: got (%q, %v), want (\"\", nil)", got, err)
	}
}

func TestResolveDockerContext_DefaultContext(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"currentContext":"default"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := resolveDockerContext()
	if err != nil || got != "" {
		t.Fatalf("default context: got (%q, %v), want (\"\", nil)", got, err)
	}
}

func TestResolveDockerContext_MissingMeta(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"currentContext":"ghost"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := resolveDockerContext()
	if err != nil || got != "" {
		t.Fatalf("missing meta: got (%q, %v), want (\"\", nil)", got, err)
	}
}

func TestResolveDockerContext_TcpEndpointSkipped(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	writeDockerContext(t, dir, "remote", "tcp://example.com:2375")
	got, err := resolveDockerContext()
	if err != nil || got != "" {
		t.Fatalf("tcp endpoint: got (%q, %v), want (\"\", nil)", got, err)
	}
}

func TestResolveDockerContext_UnixEndpoint(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	writeDockerContext(t, dir, "orbstack", "unix:///Users/foo/.orbstack/run/docker.sock")
	got, err := resolveDockerContext()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/Users/foo/.orbstack/run/docker.sock" {
		t.Fatalf("got %q, want /Users/foo/.orbstack/run/docker.sock", got)
	}
}

// TestAutoDetect_ContextWinsOverDanglingDefault reproduces the OrbStack /
// Colima / Rancher Desktop failure: DOCKER_HOST unset, the system default
// socket is a dangling symlink, but the active Docker context points at a
// live unix socket. autoDetect must use the context endpoint.
func TestAutoDetect_ContextWinsOverDanglingDefault(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")

	// Live unix socket serving /_ping and /version.
	sock := filepath.Join(shortTempDir(t), "s")
	serveUnixSocket(t, sock)

	// Docker config + meta pointing at the live socket.
	dockerCfg := filepath.Join(t.TempDir(), ".docker")
	t.Setenv("DOCKER_CONFIG", dockerCfg)
	writeDockerContext(t, dockerCfg, "test-orb", "unix://"+sock)

	// Default socket path becomes a dangling symlink.
	danglingDir := shortTempDir(t)
	dangling := filepath.Join(danglingDir, "s")
	if err := os.Symlink(filepath.Join(danglingDir, "missing"), dangling); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	prev := defaultDockerSocketPath
	defaultDockerSocketPath = dangling
	t.Cleanup(func() { defaultDockerSocketPath = prev })

	info, err := autoDetect()
	if err != nil {
		t.Fatalf("autoDetect: %v", err)
	}
	if info.SocketPath != sock {
		t.Fatalf("SocketPath = %q, want %q", info.SocketPath, sock)
	}
	if info.Type != RuntimeDocker {
		t.Fatalf("Type = %q, want %q", info.Type, RuntimeDocker)
	}
}
