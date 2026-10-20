package controller

import (
	"fmt"
	"os"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/state"
)

// saveForegroundState records a daemon state file for a foreground apply or
// serve so status, destroy, and reset see the process. The returned cleanup
// deletes the file and must run on every return path, including errors from
// BuildAndRun, or the next apply reports a stale state file.
func (sc *StackController) saveForegroundState(name, stackFile string, stack *config.Stack) (func(), error) {
	token, header, authType, issuerHost := "", "", "", ""
	if stack != nil {
		token, header, authType, issuerHost = authStateFrom(stack)
	}
	err := state.WithLock(name, 5*time.Second, func() error {
		if _, cleanErr := state.CheckAndClean(name); cleanErr != nil {
			return fmt.Errorf("checking state: %w", cleanErr)
		}
		existing, _ := state.Load(name)
		if existing != nil && state.IsRunning(existing) {
			return fmt.Errorf("stack '%s' is already running on port %d (PID: %d)\nUse 'gridctl destroy %s' to stop it first",
				name, existing.Port, existing.PID, name)
		}
		st := &state.DaemonState{
			StackName:      name,
			StackFile:      stackFile,
			PID:            os.Getpid(),
			Port:           sc.config.Port,
			StartedAt:      time.Now(),
			AuthToken:      token,
			AuthHeader:     header,
			AuthType:       authType,
			AuthIssuerHost: issuerHost,
		}
		if err := state.Save(st); err != nil {
			return fmt.Errorf("failed to save state: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return func() { _ = state.Delete(name) }, nil
}
