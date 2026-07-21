package main

import (
	"fmt"
	"strings"

	"github.com/gridctl/gridctl/pkg/state"
)

// resolveRunningPort finds the port of a running gateway, optionally
// filtered by stack name. cmdName prefixes error messages so each command
// keeps its own vocabulary. Shared by the daemon-querying commands
// (optimize, traces, activate, limits, groups), which previously carried
// verbatim copies of this resolution chain.
func resolveRunningPort(cmdName, stackName string) (int, error) {
	states, err := state.List()
	if err != nil {
		return 0, fmt.Errorf("%s: could not read state: %w", cmdName, err)
	}
	running := make([]state.DaemonState, 0, len(states))
	for _, s := range states {
		if state.IsRunning(&s) {
			running = append(running, s)
		}
	}
	if stackName != "" {
		for _, s := range running {
			if s.StackName == stackName {
				return s.Port, nil
			}
		}
		return 0, fmt.Errorf("%s: stack %q is not running", cmdName, stackName)
	}
	switch len(running) {
	case 0:
		return 0, fmt.Errorf("%s: gateway not running; try `gridctl status`", cmdName)
	case 1:
		return running[0].Port, nil
	default:
		names := make([]string, len(running))
		for i, s := range running {
			names[i] = s.StackName
		}
		return 0, fmt.Errorf("%s: multiple stacks running (%s); use --stack to pick one", cmdName, strings.Join(names, ", "))
	}
}
