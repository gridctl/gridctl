package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/provisioner"

	"github.com/charmbracelet/huh"
)

// errPromptCancelled is returned when the user aborts an interactive form;
// it surfaces as a single "cancelled" error line with exit code 1 and no
// config writes.
var errPromptCancelled = errors.New("cancelled")

// clientSelector presents a multi-select over detected clients for the
// given command ("link" or "unlink"). A package var so tests can swap in
// a fake without a PTY (the same seam pattern as open.go's browserOpener).
var clientSelector = huhSelectClients

// huhSelectClients renders the interactive client multi-select. It guards
// against non-terminal stdin itself, so command paths that never prompt
// (zero detected clients, single-client auto-unlink) keep working in
// scripts. Accessible mode (plain sequential prompts, per huh's
// convention) is honored via the ACCESSIBLE environment variable; styling
// follows the process color gate.
func huhSelectClients(command string, clients []provisioner.DetectedClient) ([]provisioner.DetectedClient, error) {
	if err := requireInteractiveStdin(command); err != nil {
		return nil, err
	}

	options := make([]huh.Option[int], len(clients))
	for i, dc := range clients {
		options[i] = huh.NewOption(fmt.Sprintf("%-18s %s", dc.Provisioner.Name(), dc.ConfigPath), i)
	}

	var picked []int
	form := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[int]().
			Title("Select clients to " + command).
			Options(options...).
			Value(&picked),
	)).WithAccessible(os.Getenv("ACCESSIBLE") != "")
	if !output.ColorEnabled(os.Stdout) {
		form = form.WithTheme(huh.ThemeBase())
	}

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil, errPromptCancelled
		}
		return nil, err
	}

	selected := make([]provisioner.DetectedClient, 0, len(picked))
	for _, i := range picked {
		selected = append(selected, clients[i])
	}
	return selected, nil
}

// requireInteractiveStdin fails fast when an interactive prompt would
// otherwise block on a non-terminal stdin (CI, pipes). The error names
// every non-interactive alternative.
func requireInteractiveStdin(command string) error {
	if output.IsTerminal(os.Stdin) {
		return nil
	}
	return fmt.Errorf("no client specified and stdin is not a terminal\nPass a client (gridctl %s claude), use --all, or run from an interactive terminal", command)
}
