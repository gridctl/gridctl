package execution

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// ExecutionConfig opts a managed MCP workload into explicit execution controls.
// Pointer collections retain the difference between omission and an empty list.
type ExecutionConfig struct {
	Mode             string            `yaml:"mode" json:"mode"`
	UID              *uint32           `yaml:"uid,omitempty" json:"uid,omitempty"`
	GID              *uint32           `yaml:"gid,omitempty" json:"gid,omitempty"`
	NoNewPrivileges  *bool             `yaml:"no_new_privileges,omitempty" json:"no_new_privileges,omitempty"`
	ReadOnly         *bool             `yaml:"read_only,omitempty" json:"read_only,omitempty"`
	DropCapabilities *[]string         `yaml:"drop_capabilities,omitempty" json:"drop_capabilities,omitempty"`
	MemoryBytes      *int64            `yaml:"memory_bytes,omitempty" json:"memory_bytes,omitempty"`
	CPUMillis        *int64            `yaml:"cpu_millis,omitempty" json:"cpu_millis,omitempty"`
	PIDs             *int64            `yaml:"pids,omitempty" json:"pids,omitempty"`
	Network          string            `yaml:"network,omitempty" json:"network,omitempty"`
	Seccomp          string            `yaml:"seccomp,omitempty" json:"seccomp,omitempty"`
	Tmpfs            *[]ExecutionTmpfs `yaml:"tmpfs,omitempty" json:"tmpfs,omitempty"`
	Mounts           *[]ExecutionMount `yaml:"mounts,omitempty" json:"mounts,omitempty"`
	Inherit          *[]string         `yaml:"inherit,omitempty" json:"inherit,omitempty"`
	Lookup           string            `yaml:"lookup,omitempty" json:"lookup,omitempty"`
}

// ExecutionTmpfs declares bounded writable scratch, never executable code.
type ExecutionTmpfs struct {
	Target    string `yaml:"target" json:"target"`
	SizeBytes int64  `yaml:"size_bytes" json:"size_bytes"`
}

// ExecutionMount declares data access. Source paths are configuration, not status.
type ExecutionMount struct {
	Source   string `yaml:"source" json:"source"`
	Target   string `yaml:"target" json:"target"`
	ReadOnly *bool  `yaml:"read_only,omitempty" json:"read_only,omitempty"`
}

// UnmarshalYAML limits strict decoding to the new execution block.
func (e *ExecutionConfig) UnmarshalYAML(node *yaml.Node) error {
	if err := executionKeys(node, "mode", "uid", "gid", "no_new_privileges", "read_only", "drop_capabilities", "memory_bytes", "cpu_millis", "pids", "network", "seccomp", "tmpfs", "mounts", "inherit", "lookup"); err != nil {
		return err
	}
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		if key != "mounts" && key != "tmpfs" {
			continue
		}
		for _, item := range value.Content {
			keys := []string{"target", "size_bytes"}
			if key == "mounts" {
				keys = []string{"source", "target", "read_only"}
			}
			if err := executionKeys(item, keys...); err != nil {
				return err
			}
		}
	}
	type plain ExecutionConfig
	if err := node.Decode((*plain)(e)); err != nil {
		return fmt.Errorf("execution: invalid field type or bound")
	}
	return nil
}

// UnmarshalJSON applies the same closed-block contract to API input.
func (e *ExecutionConfig) UnmarshalJSON(data []byte) error {
	type plain ExecutionConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode((*plain)(e)); err != nil {
		return fmt.Errorf("execution: invalid or unknown field")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("execution: expected mapping")
	}
	if fields == nil {
		return fmt.Errorf("execution: expected mapping")
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("execution: null fields are not supported")
		}
	}
	for _, key := range []string{"mounts", "tmpfs"} {
		if value, ok := fields[key]; ok {
			var items []map[string]json.RawMessage
			if err := json.Unmarshal(value, &items); err != nil {
				return fmt.Errorf("execution: expected mount mappings")
			}
			for _, item := range items {
				if item == nil {
					return fmt.Errorf("execution: expected mount mapping")
				}
				for _, value := range item {
					if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
						return fmt.Errorf("execution: null fields are not supported; omit the field instead")
					}
				}
			}
		}
	}
	return nil
}

func executionKeys(node *yaml.Node, allowed ...string) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("execution: expected mapping")
	}
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if !slices.Contains(allowed, key) || seen[key] {
			return fmt.Errorf("execution: unknown or duplicate field")
		}
		seen[key] = true
		if node.Content[i+1].Tag == "!!null" {
			return fmt.Errorf("execution: null fields are not supported; omit the field instead")
		}
	}
	return nil
}

// ExecutionContract is normalized desired configuration, not enforcement evidence.
// It contains private mount sources and must not be used as a reporting payload.
type ExecutionContract struct {
	Transport, NetworkName       string
	Port                         int
	Mode                         string
	UID, GID                     uint32
	NoNewPrivileges, ReadOnly    bool
	DropCapabilities             []string
	MemoryBytes, CPUMillis, PIDs int64
	Network, Seccomp             string
	Tmpfs                        []ExecutionTmpfs
	Mounts                       []ExecutionMount
	Inherit                      *[]string
	Lookup                       string
	Revision                     string
}

var executionEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var executionVolumeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{1,127}$`)

// ResolveExecution validates and normalizes an opt-in execution declaration.
// Omission returns nil and retains compatibility behavior.
func ResolveExecution(server Server) (*ExecutionContract, error) {
	e := server.Execution
	if e == nil {
		return nil, nil
	}
	c := &ExecutionContract{Mode: e.Mode}
	if server.External {
		return nil, fmt.Errorf("execution: externally managed transports cannot enforce local controls")
	}
	switch e.Mode {
	case "local":
		if !server.Local {
			return nil, fmt.Errorf("execution.mode: local requires a local process")
		}
		if e.UID != nil || e.GID != nil || e.NoNewPrivileges != nil || e.ReadOnly != nil || e.DropCapabilities != nil || e.MemoryBytes != nil || e.CPUMillis != nil || e.PIDs != nil || e.Network != "" || e.Seccomp != "" || e.Tmpfs != nil || e.Mounts != nil {
			return nil, fmt.Errorf("execution: container controls are inapplicable to local processes")
		}
		c.Inherit = e.Inherit
		if c.Inherit == nil {
			empty := []string{}
			c.Inherit = &empty
		} else {
			names := slices.Clone(*c.Inherit)
			slices.Sort(names)
			names = slices.Compact(names)
			c.Inherit = &names
		}
		for _, name := range *c.Inherit {
			if !executionEnvName.MatchString(name) {
				return nil, fmt.Errorf("execution.inherit: invalid environment name")
			}
		}
		c.Lookup = e.Lookup
		if c.Lookup == "" {
			c.Lookup = "absolute"
		}
		if c.Lookup != "absolute" && c.Lookup != "ambient_path" {
			return nil, fmt.Errorf("execution.lookup: must be absolute or ambient_path")
		}
		if c.Lookup == "absolute" && (len(server.Command) == 0 || !filepath.IsAbs(server.Command[0])) {
			return nil, fmt.Errorf("execution.lookup: absolute executable required")
		}
	case "hardened":
		c.Transport, c.NetworkName, c.Port = server.Transport, server.Network, server.Port
		if c.Transport == "" {
			c.Transport = "http"
		}
		if server.Local || server.External {
			return nil, fmt.Errorf("execution.mode: hardened requires a container")
		}
		if e.Inherit != nil || e.Lookup != "" {
			return nil, fmt.Errorf("execution.inherit/lookup: only valid for local processes")
		}
		if e.UID == nil || *e.UID == 0 {
			return nil, fmt.Errorf("execution.uid: explicit nonzero numeric identity required")
		}
		if e.GID == nil || *e.GID == 0 {
			return nil, fmt.Errorf("execution.gid: explicit nonzero numeric identity required")
		}
		c.UID, c.GID = *e.UID, *e.GID
		c.NoNewPrivileges, c.ReadOnly = true, true
		if e.NoNewPrivileges != nil {
			c.NoNewPrivileges = *e.NoNewPrivileges
		}
		if e.ReadOnly != nil {
			c.ReadOnly = *e.ReadOnly
		}
		c.DropCapabilities = []string{"ALL"}
		if e.DropCapabilities != nil {
			c.DropCapabilities = slices.Clone(*e.DropCapabilities)
		}
		for i, cap := range c.DropCapabilities {
			cap = strings.TrimPrefix(strings.ToUpper(cap), "CAP_")
			if !slices.Contains([]string{"ALL", "CHOWN", "DAC_OVERRIDE", "FOWNER", "FSETID", "KILL", "SETGID", "SETUID", "SETPCAP", "NET_BIND_SERVICE", "NET_RAW", "SYS_CHROOT", "MKNOD", "AUDIT_WRITE", "SETFCAP"}, cap) {
				return nil, fmt.Errorf("execution.drop_capabilities: unsupported capability")
			}
			c.DropCapabilities[i] = cap
		}
		slices.Sort(c.DropCapabilities)
		c.DropCapabilities = slices.Compact(c.DropCapabilities)
		c.MemoryBytes, c.CPUMillis, c.PIDs = 256*1024*1024, 1000, 128
		if e.MemoryBytes != nil {
			c.MemoryBytes = *e.MemoryBytes
		}
		if e.CPUMillis != nil {
			c.CPUMillis = *e.CPUMillis
		}
		if e.PIDs != nil {
			c.PIDs = *e.PIDs
		}
		if c.MemoryBytes < 6*1024*1024 || c.MemoryBytes > 1<<50 || c.MemoryBytes%4096 != 0 {
			return nil, fmt.Errorf("execution.memory_bytes: must be between 6 MiB and 1 PiB, aligned to 4 KiB")
		}
		if c.CPUMillis < 10 || c.CPUMillis > 1000000 {
			return nil, fmt.Errorf("execution.cpu_millis: must be between 10 and 1000000")
		}
		if c.PIDs < 1 || c.PIDs > 1048576 {
			return nil, fmt.Errorf("execution.pids: must be between 1 and 1048576")
		}
		c.Network = e.Network
		if c.Network == "" {
			c.Network = "none"
		}
		if c.Network != "none" && c.Network != "connected" {
			return nil, fmt.Errorf("execution.network: must be none or connected")
		}
		if c.Network == "none" && (server.Transport != "stdio" || server.Network != "" || server.Port != 0) {
			return nil, fmt.Errorf("execution.network: none requires stdio without ports or selected networks")
		}
		c.Seccomp = e.Seccomp
		if c.Seccomp == "" {
			c.Seccomp = "engine-default"
		}
		if c.Seccomp != "engine-default" {
			return nil, fmt.Errorf("execution.seccomp: only engine-default is supported")
		}
		if len(server.Volumes) != 0 {
			return nil, fmt.Errorf("execution.mounts: declare mounts here instead of volumes")
		}
		c.Tmpfs = []ExecutionTmpfs{{Target: "/tmp", SizeBytes: 64 * 1024 * 1024}}
		if e.Tmpfs != nil {
			c.Tmpfs = slices.Clone(*e.Tmpfs)
		}
		if e.Mounts != nil {
			c.Mounts = slices.Clone(*e.Mounts)
		}
		seen := map[string]bool{}
		for _, scratch := range c.Tmpfs {
			if !executionDataTarget(scratch.Target) || seen[scratch.Target] || scratch.SizeBytes < 4096 || scratch.SizeBytes%4096 != 0 || scratch.SizeBytes > c.MemoryBytes {
				return nil, fmt.Errorf("execution.tmpfs: invalid target, duplicate, or size exceeds memory bound")
			}
			seen[scratch.Target] = true
		}
		for i := range c.Mounts {
			m := &c.Mounts[i]
			if m.ReadOnly == nil {
				readOnly := true
				m.ReadOnly = &readOnly
			}
			if !executionDataTarget(m.Target) || seen[m.Target] {
				return nil, fmt.Errorf("execution.mounts: target must be a unique data location")
			}
			if !executionVolumeName.MatchString(m.Source) {
				return nil, fmt.Errorf("execution.mounts: use an explicit engine-local volume name; host binds and sockets are not supported")
			}
			seen[m.Target] = true
		}
	default:
		return nil, fmt.Errorf("execution.mode: must be hardened or local")
	}
	data, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("execution: cannot encode contract")
	}
	digest := sha256.Sum256(data)
	c.Revision = hex.EncodeToString(digest[:])
	return c, nil
}

// Server contains only fields needed to validate the execution boundary.
type Server struct {
	Execution          *ExecutionConfig
	Local, External    bool
	Command            []string
	Transport, Network string
	Port               int
	Volumes            []string
}

func executionDataTarget(target string) bool {
	return path.Clean(target) == target && (target == "/tmp" || target == "/data" || target == "/state" || strings.HasPrefix(target, "/data/") || strings.HasPrefix(target, "/state/"))
}
