// Package service installs a user-scoped supervisor unit that runs the
// gateway in the foreground and records that unit in the project lockfile
// before writing it. Rendering is pure; supervisor control is behind the
// Supervisor interface so tests never need a host session.
package service

import (
	"context"
	"fmt"
	"path/filepath"
	goruntime "runtime"
	"strconv"

	"github.com/gridctl/gridctl/pkg/runtime"
)

// Platform is the supervisor the unit targets.
type Platform string

const (
	PlatformSystemd Platform = "systemd"
	PlatformLaunchd Platform = "launchd"
)

// KV is one environment assignment, in unit order.
type KV struct {
	Key   string
	Value string
}

// Spec is the complete input to Render. It carries no I/O.
type Spec struct {
	Platform               Platform
	Name                   string // sanitized service name
	StackName              string // raw stack name
	Label                  string
	Binary                 string
	Argv                   []string
	Env                    []KV
	Home                   string
	LogPath                string
	UnitPath               string
	PodmanSocketDependency bool
	StackFile              string
	Pack                   string
	Port                   int
	BasePort               int
}

// BuildInput is the resolved install request BuildSpec turns into a Spec.
type BuildInput struct {
	// Platform overrides the host. Empty uses the host platform.
	Platform Platform
	// StackName is the raw stack name. StackFile is absolute.
	StackName string
	StackFile string
	Port      int
	BasePort  int
	Watch     bool
	// RuntimeFlag is the global --runtime value, forwarded only when set.
	RuntimeFlag string
	Home        string
	// HomeOverridden reports state.HomeOverridden so the unit path stays
	// under an overridden home.
	HomeOverridden bool
	// Environ is the process environment to filter. Nil means os.Environ
	// is not consulted here; the caller passes the slice.
	Environ []string
	// PassphraseFile is an absolute path already verified by the caller.
	// It is written as GRIDCTL_VAULT_PASSPHRASE_FILE and is never read here.
	PassphraseFile string
	// Binary, when set, is used as-is (the caller already validated
	// --binary). Empty resolves the gridctl binary.
	Binary string
	Pack   string
}

// defaultGatewayPort and defaultBasePort match apply and serve.
const (
	defaultGatewayPort = 8180
	defaultBasePort    = 9000
)

// detectRuntime is the container-runtime probe. An error never fails
// install: it only means the unit gets no Podman socket dependency.
var detectRuntime = runtime.DetectRuntime

// HostPlatform reports the supervisor for this operating system.
func HostPlatform() (Platform, error) {
	switch goruntime.GOOS {
	case "linux":
		return PlatformSystemd, nil
	case "darwin":
		return PlatformLaunchd, nil
	default:
		return "", fmt.Errorf("gridctl service is not supported on %s", goruntime.GOOS)
	}
}

// BuildSpec resolves binary, argv, filtered environment, home, unit path,
// and label. Runtime detection is advisory and never returned as an error.
func BuildSpec(ctx context.Context, in BuildInput) (Spec, []string, error) {
	if err := ctx.Err(); err != nil {
		return Spec{}, nil, err
	}
	platform := in.Platform
	if platform == "" {
		var err error
		platform, err = HostPlatform()
		if err != nil {
			return Spec{}, nil, err
		}
	}
	if in.StackName == "" {
		return Spec{}, nil, fmt.Errorf("stack name is required")
	}
	if in.StackFile == "" {
		return Spec{}, nil, fmt.Errorf("stack file is required")
	}
	name := SanitizeName(in.StackName)
	if name == "" {
		return Spec{}, nil, fmt.Errorf("stack name %q has no usable service name after sanitizing", in.StackName)
	}
	port := in.Port
	if port == 0 {
		port = defaultGatewayPort
	}
	base := in.BasePort
	if base == 0 {
		base = defaultBasePort
	}
	binary := in.Binary
	if binary == "" {
		resolved, err := ResolveBinary("")
		if err != nil {
			return Spec{}, nil, err
		}
		binary = resolved
	}
	home := in.Home
	if home == "" {
		return Spec{}, nil, fmt.Errorf("home is required")
	}
	stackFile := in.StackFile
	if abs, err := filepath.Abs(stackFile); err == nil {
		stackFile = abs
	}

	kept, skipped, err := FilterEnvironment(in.Environ, home)
	if err != nil {
		return Spec{}, nil, err
	}
	var notes []string
	for _, skip := range skipped {
		notes = append(notes, fmt.Sprintf("SSH_AUTH_SOCK=%s is not durable across reboots and was not written; use --ssh-key for private git sources", skip))
	}

	env := []KV{{Key: "GRIDCTL_HOME", Value: home}}
	pathVal := ""
	var rest []KV
	for _, kv := range kept {
		if kv.Key == "PATH" {
			pathVal = kv.Value
			continue
		}
		rest = append(rest, kv)
	}
	if pathVal == "" {
		// A user unit does not inherit the login environment. A missing
		// PATH would leave child lookups with nothing.
		pathVal = "/usr/bin:/bin"
		notes = append(notes, "PATH was unset; the unit uses /usr/bin:/bin")
	}
	env = append(env, KV{Key: "PATH", Value: pathVal})
	sortKV(rest)
	env = append(env, rest...)
	if in.PassphraseFile != "" {
		env = insertSorted(env, KV{Key: passphraseFileEnv, Value: in.PassphraseFile})
	}

	argv := []string{
		binary, "apply", stackFile,
		"--foreground", "--quiet",
		"--port", strconv.Itoa(port),
		"--base-port", strconv.Itoa(base),
	}
	if in.Watch {
		argv = append(argv, "--watch")
	}
	if in.RuntimeFlag != "" {
		argv = append(argv, "--runtime", in.RuntimeFlag)
	}

	spec := Spec{
		Platform:  platform,
		Name:      name,
		StackName: in.StackName,
		Label:     Label(name),
		Binary:    binary,
		Argv:      argv,
		Env:       env,
		Home:      home,
		LogPath:   logPath(home, in.StackName, name),
		UnitPath:  UnitPath(home, in.HomeOverridden, platform, name),
		StackFile: stackFile,
		Pack:      in.Pack,
		Port:      port,
		BasePort:  base,
	}
	spec.PodmanSocketDependency = podmanSocketDependency(ctx, in.RuntimeFlag)
	return spec, notes, nil
}

// passphraseFileEnv is the unit variable install writes. It is not copied
// from the process environment by FilterEnvironment.
const passphraseFileEnv = "GRIDCTL_VAULT_PASSPHRASE_FILE" // #nosec G101 -- environment variable name, not a secret value

func podmanSocketDependency(ctx context.Context, runtimeFlag string) bool {
	if err := ctx.Err(); err != nil {
		return false
	}
	info, err := detectRuntime(runtime.DetectOptions{Explicit: runtimeFlag})
	if err != nil || info == nil {
		return false
	}
	return info.IsRootless()
}

// logPath matches state.LogPath for a single-element stack name so
// `gridctl logs` and the launchd plist share a file. A name that is not a
// single path element uses the sanitized service name.
func logPath(home, stackName, sanitized string) string {
	name := stackName
	if name == "" || name != filepath.Base(name) || name == "." || name == ".." {
		name = sanitized
	}
	return filepath.Join(home, ".gridctl", "logs", name+".log")
}
