package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/controller"
	"github.com/gridctl/gridctl/pkg/packops"
	"github.com/gridctl/gridctl/pkg/runtime"
	"github.com/gridctl/gridctl/pkg/state"
)

// packStackLauncher is the CLI's StackLauncher. Deploy and stop write
// nothing to stdout; the pack table carries the outcome.
type packStackLauncher struct{}

func (packStackLauncher) Launch(ctx context.Context, opts packops.LaunchOptions) (packops.LaunchResult, error) {
	port := opts.Port
	if port == 0 {
		port = 8180
	}
	sc := controller.New(controller.Config{
		StackPath: opts.StackPath,
		Port:      port,
		Replace:   opts.Replace,
		Quiet:     true,
		Silent:    true,
		Runtime:   runtimeFlag,
		LogLevel:  logLevel,
	})
	if err := sc.Deploy(ctx); err != nil {
		return packops.LaunchResult{}, err
	}
	abs, err := filepath.Abs(opts.StackPath)
	if err != nil {
		return packops.LaunchResult{}, err
	}
	states, err := state.List()
	if err != nil {
		return packops.LaunchResult{}, err
	}
	for _, st := range states {
		if st.StackFile == abs && state.IsRunning(&st) {
			return packops.LaunchResult{StackName: st.StackName, Port: st.Port}, nil
		}
	}
	return packops.LaunchResult{}, fmt.Errorf("stack launched but no running daemon state was recorded for %s", opts.StackPath)
}

func (packStackLauncher) Stop(ctx context.Context, stackName string) error {
	needsRuntime := true
	var stackFile string
	err := state.WithLock(stackName, 5*time.Second, func() error {
		st, loadErr := state.Load(stackName)
		if loadErr != nil || st == nil {
			needsRuntime = false
			return nil
		}
		stackFile = st.StackFile
		if state.IsRunning(st) {
			if killErr := state.KillDaemon(st); killErr != nil {
				return killErr
			}
		}
		return state.Delete(stackName)
	})
	if err != nil {
		return err
	}
	if stackFile != "" {
		if stack, loadErr := config.LoadStack(stackFile); loadErr == nil {
			needsRuntime = stack.NeedsContainerRuntime()
		}
	}
	rt, rtErr := runtime.New()
	if rtErr != nil {
		if needsRuntime {
			return rtErr
		}
		return nil
	}
	defer rt.Close()
	if downErr := rt.Down(ctx, stackName); downErr != nil && needsRuntime {
		return downErr
	}
	return nil
}

func packStoredVariables() (map[string]bool, bool) {
	store, err := loadVault()
	if err != nil || store.IsLocked() {
		return nil, false
	}
	keys := map[string]bool{}
	for _, variable := range store.List() {
		keys[variable.Key] = true
	}
	return keys, true
}
