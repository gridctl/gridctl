package main

import (
	"fmt"
	"os"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/controller"

	"github.com/spf13/cobra"
)

var (
	overrideFlag     string
	noOverrideFlag   bool
	overrideOptional bool
)

func addOverrideFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&overrideFlag, "override", "", "Override file merged after extends (default: stack.override.yaml beside the stack)")
	cmd.Flags().BoolVar(&noOverrideFlag, "no-override", false, "Ignore override files")
	cmd.Flags().BoolVar(&overrideOptional, "override-optional", false, "Allow a selected override file to be absent")
	_ = cmd.Flags().MarkHidden("override-optional")
}

// applyOverrideFlags copies the process-wide override flags onto cfg.
// An explicit --override path must exist. --override and --no-override
// cannot be combined.
func applyOverrideFlags(cfg *controller.Config) error {
	if overrideFlag != "" && noOverrideFlag {
		return fmt.Errorf("--override and --no-override are mutually exclusive")
	}
	if noOverrideFlag {
		cfg.NoOverride = true
		return nil
	}
	if overrideFlag == "" {
		return nil
	}
	info, err := os.Stat(overrideFlag)
	if err != nil {
		if os.IsNotExist(err) {
			if overrideOptional {
				cfg.OverridePath = overrideFlag
				return nil
			}
			return fmt.Errorf("override: %s: file does not exist", overrideFlag)
		}
		return fmt.Errorf("override: %s: %w", overrideFlag, err)
	}
	if info.IsDir() {
		return fmt.Errorf("override: %s: is a directory", overrideFlag)
	}
	cfg.OverridePath = overrideFlag
	cfg.OverrideRequired = !overrideOptional
	return nil
}

func planOverrideOptions() []config.LoadOption {
	if noOverrideFlag {
		return []config.LoadOption{config.WithoutOverride()}
	}
	if overrideFlag != "" {
		return []config.LoadOption{config.WithOverride(overrideFlag), config.RequireOverride()}
	}
	return nil
}
