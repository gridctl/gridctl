package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gridctl/gridctl/internal/stackedit"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/packops"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/reload"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/spf13/cobra"
)

const enableHTTPTimeout = 60 * time.Second

var (
	enableFormat     string
	enableOverride   string
	enableNoOverride bool
)

func init() {
	for _, cmd := range []*cobra.Command{enableCmd, disableCmd} {
		cmd.Flags().StringVar(&enableFormat, "format", "text", "Output format: text or json")
		cmd.Flags().StringVar(&enableOverride, "override", "", "Write enabled to this override file instead of the stack")
		cmd.Flags().BoolVar(&enableNoOverride, "no-override", false, "Write the base stack file even when an override exists")
	}
}

var enableCmd = &cobra.Command{
	Use:   "enable <stack> <server>",
	Short: "Enable an MCP server and reload",
	Long: `Enables a declared MCP server.

The stack argument is a running stack name or a stack file path. The change
is written to the base file, or to the override file when the stack is a
pinned pack checkout, an override is already active, or --override is set.
A running stack is reloaded. A stopped stack applies the change on the next
gridctl apply.

Exit 0 when the server is enabled or was already enabled. Exit 1 when the
write or reload fails. Exit 2 when the server is unknown or the edited
stack does not validate.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runEnable(args[0], args[1], true)
	},
}

var disableCmd = &cobra.Command{
	Use:   "disable <stack> <server>",
	Short: "Disable an MCP server and reload",
	Long: `Disables a declared MCP server without removing it.

The server stays in the stack and is validated, but it is not started or
registered. Pins are kept. See gridctl enable for write-target and exit rules.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runEnable(args[0], args[1], false)
	},
}

type enableReport struct {
	Stack    string               `json:"stack"`
	Server   string               `json:"server"`
	Enabled  bool                 `json:"enabled"`
	Changed  bool                 `json:"changed"`
	File     string               `json:"file"`
	Backup   string               `json:"backup,omitempty"`
	Reloaded bool                 `json:"reloaded"`
	Reload   *reload.ReloadResult `json:"reload"`
	Target   string               `json:"target"`
}

func runEnable(stackArg, server string, enabled bool) error {
	if enableFormat != "text" && enableFormat != "json" {
		return fmt.Errorf("output format must be text or json")
	}
	if enableOverride != "" && enableNoOverride {
		return refuseEnable(fmt.Errorf("--override and --no-override are mutually exclusive"))
	}
	file, running, err := resolveEnableStack(stackArg)
	if err != nil {
		return refuseEnable(err)
	}
	targetPath, targetKind, err := selectEnableTarget(file, running)
	if err != nil {
		return refuseEnable(err)
	}
	report, err := writeEnabled(file, targetPath, targetKind, server, enabled)
	if err != nil {
		if errors.Is(err, stackedit.ErrServerNotFound) || errors.Is(err, errEnableRefused) {
			return refuseEnable(err)
		}
		return err
	}
	if running != nil && report.Changed {
		result, reloadErr := postReload(running)
		report.Reloaded = reloadErr == nil && result != nil && result.Success
		report.Reload = result
		if reloadErr != nil || (result != nil && !result.Success) {
			printEnableReport(report, enabled)
			msg := "reload failed; the file was written and is retained"
			if reloadErr != nil {
				return fmt.Errorf("%s: %w", msg, reloadErr)
			}
			return fmt.Errorf("%s: %s", msg, result.Message)
		}
	}
	printEnableReport(report, enabled)
	return nil
}

var errEnableRefused = errors.New("refused")

func refuseEnable(err error) error {
	if enableFormat == "json" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"error": err.Error(),
		})
	}
	fmt.Fprintln(os.Stderr, err.Error())
	os.Exit(2)
	return err
}

func resolveEnableStack(arg string) (string, *state.DaemonState, error) {
	if st, err := state.Load(arg); err == nil && st != nil && st.StackFile != "" {
		if state.IsRunning(st) {
			return st.StackFile, st, nil
		}
		return st.StackFile, nil, nil
	}
	if info, err := os.Stat(arg); err == nil && !info.IsDir() {
		abs, err := filepath.Abs(arg)
		if err != nil {
			return "", nil, err
		}
		data, readErr := os.ReadFile(abs)
		if readErr != nil {
			return "", nil, readErr
		}
		stack, err := config.DecodeStack(data)
		if err != nil {
			return "", nil, fmt.Errorf("stack %q is not a running stack name or a valid stack file: %w", arg, err)
		}
		if stack.Name != "" {
			if st, err := state.Load(stack.Name); err == nil && st != nil && state.IsRunning(st) && samePlanPath(st.StackFile, abs) {
				return abs, st, nil
			}
		}
		return abs, nil, nil
	}
	return "", nil, fmt.Errorf("stack %q is not a running stack name or a stack file path", arg)
}

func selectEnableTarget(stackFile string, running *state.DaemonState) (path, kind string, err error) {
	if enableOverride != "" {
		return enableOverride, "override", nil
	}
	home, homeErr := state.Home()
	if homeErr == nil {
		if _, override, ok := packops.LockedPackOverride(home, skills.LockFilePath(), stackFile); ok {
			return override, "override", nil
		}
	}
	if !enableNoOverride && running != nil {
		if reported := daemonOverride(running); reported != "" {
			return reported, "override", nil
		}
	}
	if !enableNoOverride && running == nil {
		sibling := filepath.Join(filepath.Dir(stackFile), config.OverrideFileName)
		if info, statErr := os.Stat(sibling); statErr == nil && !info.IsDir() {
			return sibling, "override", nil
		}
	}
	return stackFile, "stack", nil
}

func daemonOverride(st *state.DaemonState) string {
	if st == nil {
		return ""
	}
	api := newDaemonAPIFor(*st, 2*time.Second)
	resp, err := api.Get(fmt.Sprintf("http://localhost:%d/api/status", st.Port))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var body struct {
		Override string `json:"override"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return ""
	}
	return body.Override
}

func writeEnabled(stackFile, targetPath, kind, server string, enabled bool) (*enableReport, error) {
	if kind == "stack" {
		data, readErr := os.ReadFile(stackFile)
		if readErr != nil {
			return nil, readErr
		}
		names, err := stackServerNames(data)
		if err != nil {
			return nil, err
		}
		if !names[server] {
			return nil, fmt.Errorf("%w: server %q is not declared in %s", stackedit.ErrServerNotFound, server, stackFile)
		}
	} else if err := serverDeclared(stackFile, server); err != nil {
		return nil, err
	}
	mu := stackedit.PathLock(targetPath)
	mu.Lock()
	defer mu.Unlock()

	var original []byte
	if info, err := os.Stat(targetPath); err == nil && !info.IsDir() {
		original, err = os.ReadFile(targetPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", targetPath, err)
		}
	}
	var updated []byte
	var changed bool
	var err error
	if kind == "override" {
		updated, changed, err = stackedit.SetOverrideEnabled(original, server, enabled)
	} else {
		updated, changed, err = stackedit.SetServerEnabled(original, server, enabled)
	}
	if err != nil {
		return nil, err
	}
	report := &enableReport{
		Stack:   stackNameOf(stackFile),
		Server:  server,
		Enabled: enabled,
		Changed: changed,
		File:    targetPath,
		Target:  kind,
	}
	if !changed {
		return report, nil
	}
	if kind == "stack" {
		if err := validateEditedStack(updated); err != nil {
			return nil, fmt.Errorf("%w: %s", errEnableRefused, err.Error())
		}
	} else if err := validateEditedOverride(stackFile, updated, targetPath); err != nil {
		return nil, fmt.Errorf("%w: %s", errEnableRefused, err.Error())
	}
	if len(original) > 0 {
		backup, err := provisioner.CreateBackup(targetPath)
		if err != nil {
			return nil, fmt.Errorf("backing up %s: %w", targetPath, err)
		}
		report.Backup = backup
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return nil, err
	}
	if err := stackedit.AtomicWrite(targetPath, updated); err != nil {
		return nil, err
	}
	return report, nil
}

func serverDeclared(stackFile, name string) error {
	data, err := os.ReadFile(stackFile)
	if err != nil {
		return err
	}
	names, err := stackServerNames(data)
	if err != nil {
		return err
	}
	if !names[name] {
		return fmt.Errorf("%w: server %q is not declared in %s", stackedit.ErrServerNotFound, name, stackFile)
	}
	return nil
}

func stackNameOf(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return filepath.Base(path)
	}
	stack, err := config.DecodeStack(data)
	if err != nil || stack.Name == "" {
		return filepath.Base(path)
	}
	return stack.Name
}

func validateEditedStack(data []byte) error {
	stack, err := config.DecodeStack(data)
	if err != nil {
		return fmt.Errorf("post-edit stack does not parse: %w", err)
	}
	if err := config.ExpandStackVarsWithEnvChecked(stack); err != nil {
		return fmt.Errorf("post-edit stack variable resolution failed; nothing written: %w", err)
	}
	stack.SetDefaults()
	result := config.ValidateWithIssues(stack)
	if result.Valid {
		return nil
	}
	return fmt.Errorf("post-edit stack fails validation; nothing written: %s", result.Issues[0].Message)
}

func validateEditedOverride(stackFile string, overrideData []byte, overridePath string) error {
	ov, err := config.DecodeOverride(overrideData, overridePath)
	if err != nil {
		return err
	}
	base, err := os.ReadFile(stackFile)
	if err != nil {
		return err
	}
	stack, err := config.DecodeStack(base)
	if err != nil {
		return fmt.Errorf("base stack does not parse: %w", err)
	}
	if err := config.ApplyOverride(stack, ov, overridePath); err != nil {
		return err
	}
	stack.SetDefaults()
	result := config.ValidateWithIssues(stack)
	if !result.Valid {
		return fmt.Errorf("merged stack fails validation; nothing written: %s", result.Issues[0].Message)
	}
	return nil
}

func postReload(st *state.DaemonState) (*reload.ReloadResult, error) {
	api := newDaemonAPIFor(*st, enableHTTPTimeout)
	resp, err := api.Do(http.MethodPost, fmt.Sprintf("http://localhost:%d/api/reload", st.Port), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var result reload.ReloadResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("reload response: %s", string(body))
	}
	if resp.StatusCode >= 400 && result.Message == "" {
		result.Success = false
		result.Message = string(body)
	}
	return &result, nil
}

func printEnableReport(report *enableReport, enabled bool) {
	if enableFormat == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
		return
	}
	verb := "enabled"
	if !enabled {
		verb = "disabled"
	}
	if !report.Changed {
		fmt.Printf("%s %q is already %s (%s)\n", report.Server, report.File, verb, report.Target)
		return
	}
	fmt.Printf("%s %q in %s (%s)\n", verb, report.Server, report.File, report.Target)
	if report.Reloaded {
		fmt.Println("reloaded")
	} else if report.Reload == nil {
		fmt.Println("change applies on the next apply")
	}
}
