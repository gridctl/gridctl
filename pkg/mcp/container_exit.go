package mcp

import (
	"context"
	"strings"
	"time"
)

// containerInspectTimeout bounds a health-check inspect so a slow runtime
// cannot stall the monitor. The call runs outside healthMu.
const containerInspectTimeout = 2 * time.Second

// ContainerExit is the runtime's account of why a container replica stopped.
type ContainerExit struct {
	Code       int        `json:"code"`
	OOMKilled  bool       `json:"oomKilled"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Status     string     `json:"status"`          // runtime status string, e.g. "exited", "dead"
	Error      string     `json:"error,omitempty"` // runtime-reported error, if any
}

// containerInspector is implemented by clients that can inspect their container.
// A nil result with a nil error means the container is still running.
type containerInspector interface {
	InspectContainer(ctx context.Context) (*ContainerExit, error)
}

func parseContainerFinishedAt(raw string) (time.Time, bool) {
	if raw == "" || strings.HasPrefix(raw, "0001-01-01") {
		return time.Time{}, false
	}
	finished, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || finished.IsZero() || finished.Year() <= 1 {
		return time.Time{}, false
	}
	return finished, true
}
