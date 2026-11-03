package hooksync

import "strings"

// UnenforcedHook is one required hook that is not in-sync for a client.
type UnenforcedHook struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// Unenforced reports required hooks that are not enforced on client.
// Reason is one of "not projected", "drifted", "stale", "pin drift", or
// "client has no hook target". State and exit codes are the caller's.
func Unenforced(client string, required []string, statuses []ProjectionStatus) []UnenforcedHook {
	if len(required) == 0 {
		return nil
	}
	if _, ok := FindTarget(client); !ok {
		out := make([]UnenforcedHook, 0, len(required))
		for _, name := range required {
			out = append(out, UnenforcedHook{Name: name, Reason: "client has no hook target"})
		}
		return out
	}
	byName := map[string]ProjectionStatus{}
	for _, s := range statuses {
		if s.Client == client {
			byName[s.Hook] = s
		}
	}
	var out []UnenforcedHook
	for _, name := range required {
		s, ok := byName[name]
		if !ok || s.State == StateTargetMissing || s.State == "" {
			out = append(out, UnenforcedHook{Name: name, Reason: "not projected"})
			continue
		}
		switch s.State {
		case StateInSync:
		case StateDrifted:
			out = append(out, UnenforcedHook{Name: name, Reason: "drifted"})
		case StateStale:
			reason := "stale"
			if strings.Contains(s.Detail, "pin drift") {
				reason = "pin drift"
			}
			out = append(out, UnenforcedHook{Name: name, Reason: reason})
		default:
			out = append(out, UnenforcedHook{Name: name, Reason: "not projected"})
		}
	}
	return out
}
