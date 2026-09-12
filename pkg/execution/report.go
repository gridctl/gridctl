package execution

import (
	"strconv"
	"strings"
	"time"
)

// Control reports allowlisted metadata, never raw engine or process output.
type Control struct {
	Field     string `json:"field"`
	Requested string `json:"requested"`
	Observed  string `json:"observed,omitempty"`
	Outcome   string `json:"outcome"`
	Source    string `json:"source"`
}

// Report is a launch/admission snapshot, not a continuous protection claim.
type Report struct {
	EndpointPort   int       `json:"endpoint_port,omitempty"`
	Mode           string    `json:"mode"`
	Revision       string    `json:"revision"`
	Instance       string    `json:"instance"`
	Outcome        string    `json:"outcome"`
	Eligible       bool      `json:"eligible"`
	ObservedAt     time.Time `json:"observed_at"`
	Runtime        string    `json:"runtime"`
	RuntimeContext string    `json:"runtime_context,omitempty"`
	DaemonRootless string    `json:"daemon_rootless"`
	UserNamespace  string    `json:"user_namespace"`
	Controls       []Control `json:"controls"`
}

// RequestedReport returns value-free desired settings with no runtime eligibility.
func RequestedReport(c *ExecutionContract) *Report {
	if c == nil {
		return nil
	}
	r := &Report{Mode: c.Mode, Revision: c.Revision, Outcome: "pending", Runtime: "unknown", DaemonRootless: "unknown", UserNamespace: "unknown"}
	values := []struct{ field, value string }{{"mode", c.Mode}}
	if c.Mode == "hardened" {
		drops := strings.Join(c.DropCapabilities, ", ")
		if drops == "" {
			drops = "none (exception)"
		}
		values = append(values, []struct{ field, value string }{
			{"uid", strconv.FormatUint(uint64(c.UID), 10)}, {"gid", strconv.FormatUint(uint64(c.GID), 10)},
			{"privileged", "false"}, {"pid_namespace", "private"}, {"drop_capabilities", drops}, {"swap_bytes", "0"},
			{"read_only", strconv.FormatBool(c.ReadOnly)}, {"no_new_privileges", strconv.FormatBool(c.NoNewPrivileges)},
			{"memory_bytes", strconv.FormatInt(c.MemoryBytes, 10)}, {"cpu_millis", strconv.FormatInt(c.CPUMillis, 10)}, {"pids", strconv.FormatInt(c.PIDs, 10)},
			{"network", c.Network}, {"seccomp", c.Seccomp}, {"data_mount_count", strconv.Itoa(len(c.Mounts))}, {"scratch_mount_count", strconv.Itoa(len(c.Tmpfs))},
		}...)
		for i, scratch := range c.Tmpfs {
			values = append(values, struct{ field, value string }{"tmpfs[" + strconv.Itoa(i) + "]", strconv.FormatInt(scratch.SizeBytes, 10) + " bytes; noexec, nodev, nosuid"})
		}
		for i, mount := range c.Mounts {
			access := "read-only data volume"
			if mount.ReadOnly != nil && !*mount.ReadOnly {
				access = "writable data volume; storage not bounded"
			}
			values = append(values, struct{ field, value string }{"mounts[" + strconv.Itoa(i) + "]", access})
		}
	} else {
		values = append(values, struct{ field, value string }{"lookup", c.Lookup})
		if c.Inherit != nil {
			values = append(values, struct{ field, value string }{"inherit_names", strings.Join(*c.Inherit, ", ")})
		}
	}
	for _, value := range values {
		r.Controls = append(r.Controls, Control{Field: value.field, Requested: value.value, Outcome: "configured", Source: "normalized desired contract"})
	}
	return r
}
