package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

const pinsAPITimeout = 10 * time.Second

// Exit codes match the conventions in cmd/gridctl/optimize.go and
// cmd/gridctl/validate.go so CI scripts can rely on a stable contract.
const (
	pinsExitOK             = 0
	pinsExitDrift          = 1
	pinsExitInfrastructure = 2
)

// pinsJSONSchemaVersion identifies the shape of the pins list/verify JSON
// documents. Evolution within a version is append-only.
const pinsJSONSchemaVersion = 1

var (
	pinsStack        string
	pinsExitCode     bool
	pinsListFormat   string
	pinsListJSON     *bool
	pinsVerifyFormat string
	pinsVerifyJSON   *bool
)

var pinsCmd = &cobra.Command{
	Use:   "pins",
	Short: "Manage schema pins for MCP servers",
	Long:  "Inspect, verify, approve, and reset TOFU schema pins that protect against rug pull attacks.",
}

var pinsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pin status for all servers in a stack",
	Long: `List pin status for all servers in a stack.

Default output is a styled table; use '--format json' for machine-readable
output.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(pinsListFormat, cmd.Flags().Changed("format"), *pinsListJSON)
		if err != nil {
			return err
		}
		return runPinsList(format)
	},
}

var pinsVerifyCmd = &cobra.Command{
	Use:   "verify [server]",
	Short: "Show verification status for servers",
	Long: `Show pin verification status.

Default output is one line per server; use '--format json' for a
machine-readable document with a top-level has_drift flag.

Exit codes:
  0  all pins verified (or nothing pinned yet)
  1  drift detected
  2  infrastructure error (no stack, unreadable pin store, unknown server)`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(pinsVerifyFormat, cmd.Flags().Changed("format"), *pinsVerifyJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(pinsExitInfrastructure)
		}
		server := ""
		if len(args) == 1 {
			server = args[0]
		}
		stackName, servers, err := loadPinsForCLI()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(pinsExitInfrastructure)
		}
		if exit := pinsVerifyExit(os.Stdout, os.Stderr, stackName, servers, server, format); exit != pinsExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

var pinsApproveCmd = &cobra.Command{
	Use:   "approve <server>",
	Short: "Approve schema changes for a server",
	Long:  "Re-pin the current tool definitions for a server, clearing drift status.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPinsApprove(args[0])
	},
}

var pinsResetCmd = &cobra.Command{
	Use:   "reset <server>",
	Short: "Delete pins for a server",
	Long:  "Remove all pins for a server. It will be re-pinned on next deploy.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPinsReset(args[0])
	},
}

func init() {
	pinsCmd.PersistentFlags().StringVar(&pinsStack, "stack", "", "Stack name (auto-detected if only one stack is deployed)")

	pinsListCmd.Flags().StringVar(&pinsListFormat, "format", "", "Output format: 'json' for machine-readable output (default: table)")
	pinsListJSON = addJSONAlias(pinsListCmd)

	pinsVerifyCmd.Flags().StringVar(&pinsVerifyFormat, "format", "", "Output format: 'json' for machine-readable output (default: text)")
	pinsVerifyJSON = addJSONAlias(pinsVerifyCmd)

	// Kept so existing CI invocations don't break; drift exits 1 regardless.
	pinsVerifyCmd.Flags().BoolVar(&pinsExitCode, "exit-code", false, "Exit with code 1 if drift is detected (for CI)")
	_ = pinsVerifyCmd.Flags().MarkDeprecated("exit-code", "drift now exits 1 by default")

	pinsCmd.AddCommand(pinsListCmd)
	pinsCmd.AddCommand(pinsVerifyCmd)
	pinsCmd.AddCommand(pinsApproveCmd)
	pinsCmd.AddCommand(pinsResetCmd)
}

// resolveStack returns the stack name, auto-detecting when only one stack is deployed.
func resolveStack() (string, error) {
	if pinsStack != "" {
		return pinsStack, nil
	}

	states, err := state.List()
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("listing stacks: %w", err)
	}

	switch len(states) {
	case 0:
		return "", fmt.Errorf("no running stack found. Deploy a stack first")
	case 1:
		return states[0].StackName, nil
	default:
		names := make([]string, len(states))
		for i, s := range states {
			names[i] = s.StackName
		}
		return "", fmt.Errorf("multiple stacks found %v. Use --stack to specify one", names)
	}
}

// resolveRunningStack returns the daemon state for the active stack.
func resolveRunningStack() (*state.DaemonState, error) {
	stackName, err := resolveStack()
	if err != nil {
		return nil, err
	}

	st, err := state.Load(stackName)
	if err != nil {
		return nil, fmt.Errorf("stack '%s' not found", stackName)
	}

	if !state.IsRunning(st) {
		return nil, fmt.Errorf("stack '%s' is not running. Deploy the stack first", st.StackName)
	}

	return st, nil
}

// pinsListDoc is the machine-readable document emitted by `pins list --format json`.
// Server records carry the same shape as GET /api/pins.
type pinsListDoc struct {
	SchemaVersion int                         `json:"schema_version"`
	Stack         string                      `json:"stack"`
	Servers       map[string]*pins.ServerPins `json:"servers"`
}

// pinsVerifyServer is one server's entry in the verify JSON document.
type pinsVerifyServer struct {
	Name           string    `json:"name"`
	Status         string    `json:"status"`
	ToolCount      int       `json:"tool_count"`
	LastVerifiedAt time.Time `json:"last_verified_at"`
}

// pinsVerifyDoc is the machine-readable document emitted by `pins verify --format json`.
type pinsVerifyDoc struct {
	SchemaVersion int                `json:"schema_version"`
	Stack         string             `json:"stack"`
	HasDrift      bool               `json:"has_drift"`
	Servers       []pinsVerifyServer `json:"servers"`
}

// buildPinsVerifyDoc assembles the verify report from stored pin records,
// sorted by server name. Drift is judged from the persisted status, matching
// what the gateway last observed.
func buildPinsVerifyDoc(stackName string, servers map[string]*pins.ServerPins) pinsVerifyDoc {
	doc := pinsVerifyDoc{
		SchemaVersion: pinsJSONSchemaVersion,
		Stack:         stackName,
		Servers:       make([]pinsVerifyServer, 0, len(servers)),
	}
	for _, name := range sortedMapKeys(servers) {
		sp := servers[name]
		doc.Servers = append(doc.Servers, pinsVerifyServer{
			Name:           name,
			Status:         sp.Status,
			ToolCount:      sp.ToolCount,
			LastVerifiedAt: sp.LastVerifiedAt,
		})
		if sp.Status == pins.StatusDrift {
			doc.HasDrift = true
		}
	}
	return doc
}

// renderPinsVerifyText prints the human one-line-per-server view.
func renderPinsVerifyText(w io.Writer, doc pinsVerifyDoc) {
	for _, sv := range doc.Servers {
		switch sv.Status {
		case pins.StatusDrift:
			fmt.Fprintf(w, "  ✗ %-24s drift detected\n", sv.Name)
		case pins.StatusApprovedPendingRedeploy:
			fmt.Fprintf(w, "  ~ %-24s %d tools approved, pending redeploy\n", sv.Name, sv.ToolCount)
		default:
			fmt.Fprintf(w, "  ✓ %-24s %d tools verified\n", sv.Name, sv.ToolCount)
		}
	}
}

// loadPinsForCLI resolves the stack and reads its pin store from disk.
func loadPinsForCLI() (string, map[string]*pins.ServerPins, error) {
	stackName, err := resolveStack()
	if err != nil {
		return "", nil, err
	}
	ps := pins.New(stackName)
	if err := ps.Load(); err != nil {
		return "", nil, err
	}
	return stackName, ps.GetAll(), nil
}

func runPinsList(format string) error {
	stackName, servers, err := loadPinsForCLI()
	if err != nil {
		return err
	}

	if strings.EqualFold(format, "json") {
		return output.EncodeJSON(os.Stdout, pinsListDoc{
			SchemaVersion: pinsJSONSchemaVersion,
			Stack:         stackName,
			Servers:       servers,
		})
	}

	if len(servers) == 0 {
		fmt.Printf("No pins found for stack '%s'. Deploy the stack first.\n", stackName)
		return nil
	}

	names := sortedMapKeys(servers)

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleRounded)
	t.AppendHeader(table.Row{"SERVER", "TOOLS", "STATUS", "LAST VERIFIED"})

	for _, name := range names {
		sp := servers[name]
		t.AppendRow(table.Row{
			name,
			sp.ToolCount,
			pinStatusLabel(sp.Status),
			sp.LastVerifiedAt.Format("2006-01-02 15:04:05"),
		})
	}

	t.Render()
	return nil
}

// pinsVerifyExit renders the verify report and returns the process exit code:
// 0 all verified (or nothing pinned yet), 1 drift detected, 2 infrastructure
// error. An empty store is the normal pre-pin state after a fresh deploy, so
// it succeeds; a named server absent from a non-empty store is an error, since
// verifying it is impossible and a typo should not pass a CI gate.
func pinsVerifyExit(stdout, stderr io.Writer, stackName string, servers map[string]*pins.ServerPins, server, format string) int {
	asJSON := strings.EqualFold(format, "json")

	if len(servers) == 0 {
		if asJSON {
			if err := output.EncodeJSON(stdout, buildPinsVerifyDoc(stackName, servers)); err != nil {
				fmt.Fprintln(stderr, err)
				return pinsExitInfrastructure
			}
		} else {
			fmt.Fprintf(stdout, "No pins found for stack '%s'. Deploy the stack first.\n", stackName)
		}
		return pinsExitOK
	}

	if server != "" {
		sp, ok := servers[server]
		if !ok {
			fmt.Fprintf(stderr, "no pins found for server %q. Deploy the stack first\n", server)
			return pinsExitInfrastructure
		}
		servers = map[string]*pins.ServerPins{server: sp}
	}

	doc := buildPinsVerifyDoc(stackName, servers)

	if asJSON {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return pinsExitInfrastructure
		}
	} else {
		renderPinsVerifyText(stdout, doc)
	}

	if doc.HasDrift {
		return pinsExitDrift
	}
	return pinsExitOK
}

func runPinsApprove(server string) error {
	st, err := resolveRunningStack()
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://localhost:%d/api/pins/%s/approve", st.Port, server)
	client := &http.Client{Timeout: pinsAPITimeout}

	resp, err := client.Post(url, "application/json", nil)
	if err != nil {
		return fmt.Errorf("calling pins API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("no pins found for server %q. Deploy the stack first", server)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("approve failed: %s", string(body))
	}

	var result struct {
		ToolCount int    `json:"tool_count"`
		Server    string `json:"server"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	fmt.Printf("✓ Approved schema update for %s (%d tools re-pinned)\n", server, result.ToolCount)
	return nil
}

func runPinsReset(server string) error {
	st, err := resolveRunningStack()
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://localhost:%d/api/pins/%s", st.Port, server)
	client := &http.Client{Timeout: pinsAPITimeout}

	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("calling pins API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("no pins found for server %q. Deploy the stack first", server)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("reset failed: %s", string(body))
	}

	fmt.Printf("✓ Pins reset for %s. Server will be re-pinned on next deploy.\n", server)
	return nil
}

// pinStatusLabel returns a human-readable display string for a pin status.
func pinStatusLabel(status string) string {
	switch status {
	case pins.StatusPinned:
		return "✓ pinned"
	case pins.StatusDrift:
		return "⚠ drift"
	case pins.StatusApprovedPendingRedeploy:
		return "~ approved"
	default:
		return "— " + status
	}
}

// sortedMapKeys returns sorted keys from a string-keyed map.
func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
