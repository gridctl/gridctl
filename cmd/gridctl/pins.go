package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

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
	pinsStack         string
	pinsExitCode      bool
	pinsListFormat    string
	pinsListJSON      *bool
	pinsListPlain     *bool
	pinsVerifyFormat  string
	pinsVerifyJSON    *bool
	pinsDiffFormat    string
	pinsDiffJSON      *bool
	pinsApproveExpect string
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
		if err := resolvePlain(*pinsListPlain, format); err != nil {
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

var pinsDiffCmd = &cobra.Command{
	Use:   "diff [server]",
	Short: "Show what changed for drifted servers",
	Long: `Show the per-tool delta between pinned and live tool definitions.

Without an argument, recomputes the diff for every pinned server against its
live definitions and reports the servers with changes; servers that cannot be
diffed (e.g. removed from the stack) are skipped with a warning. With a server
name, diffs that one server regardless of status. Requires a running stack,
since the live definitions come from the gateway.

Default output is a per-tool before/after view with control characters
escaped (poisoned descriptions hide instructions in invisible characters);
use '--format json' for a machine-readable document. The JSON includes each
server's live_server_hash for 'pins approve --expect'.

Exit codes:
  0  no drift
  1  drift detected
  2  infrastructure error (no running stack, unknown server, API failure,
     or servers skipped with warnings)`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(pinsDiffFormat, cmd.Flags().Changed("format"), *pinsDiffJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(pinsExitInfrastructure)
		}
		server := ""
		if len(args) == 1 {
			server = args[0]
		}
		st, err := resolveRunningStack()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(pinsExitInfrastructure)
		}
		doc, warnings, err := buildPinsDiffDoc(st, server)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(pinsExitInfrastructure)
		}
		if exit := pinsDiffExit(os.Stdout, os.Stderr, doc, warnings, format); exit != pinsExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

var pinsApproveCmd = &cobra.Command{
	Use:   "approve <server>",
	Short: "Approve schema changes for a server",
	Long: `Re-pin the current tool definitions for a server, clearing drift status.

Pass --expect with the live_server_hash from 'pins diff --format json' to
bind the approval to the reviewed definitions: if the server's tools change
again between review and approval, the approve is rejected instead of
silently pinning definitions nobody saw.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPinsApprove(args[0], pinsApproveExpect)
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
	pinsListPlain = addPlainFlag(pinsListCmd)

	pinsVerifyCmd.Flags().StringVar(&pinsVerifyFormat, "format", "", "Output format: 'json' for machine-readable output (default: text)")
	pinsVerifyJSON = addJSONAlias(pinsVerifyCmd)

	// Kept so existing CI invocations don't break; drift exits 1 regardless.
	pinsVerifyCmd.Flags().BoolVar(&pinsExitCode, "exit-code", false, "Exit with code 1 if drift is detected (for CI)")
	_ = pinsVerifyCmd.Flags().MarkDeprecated("exit-code", "drift now exits 1 by default")

	pinsDiffCmd.Flags().StringVar(&pinsDiffFormat, "format", "", "Output format: 'json' for machine-readable output (default: text)")
	pinsDiffJSON = addJSONAlias(pinsDiffCmd)

	pinsApproveCmd.Flags().StringVar(&pinsApproveExpect, "expect", "", "Reviewed live_server_hash from 'pins diff'; approval is rejected if the live definitions no longer match")

	pinsCmd.AddCommand(pinsListCmd)
	pinsCmd.AddCommand(pinsVerifyCmd)
	pinsCmd.AddCommand(pinsDiffCmd)
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

	t := output.NewTableWriter(os.Stdout, *pinsListPlain)
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

// pinsToolDiff is one modified tool in the diff document, mirroring the
// GET /api/pins/{server}/diff wire shape.
type pinsToolDiff struct {
	Name           string `json:"name"`
	OldHash        string `json:"old_hash"`
	NewHash        string `json:"new_hash"`
	OldDescription string `json:"old_description"`
	NewDescription string `json:"new_description"`
}

// pinsDiffServer is one server's delta in the diff document. LiveServerHash
// fingerprints the live definitions; pass it to 'pins approve --expect' to
// bind the approval to this reviewed snapshot.
type pinsDiffServer struct {
	Name           string         `json:"name"`
	Status         string         `json:"status"`
	LiveServerHash string         `json:"live_server_hash"`
	ModifiedTools  []pinsToolDiff `json:"modified_tools"`
	NewTools       []string       `json:"new_tools"`
	RemovedTools   []string       `json:"removed_tools"`
}

// pinsDiffDoc is the machine-readable document emitted by `pins diff --format json`.
type pinsDiffDoc struct {
	SchemaVersion int              `json:"schema_version"`
	Stack         string           `json:"stack"`
	HasDrift      bool             `json:"has_drift"`
	Servers       []pinsDiffServer `json:"servers"`
}

// apiErrorMessage extracts the "error" field from a writeJSONError body,
// falling back to the raw body so the daemon's actual reason is never hidden.
func apiErrorMessage(body []byte, fallback string) string {
	var wire struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &wire); err == nil && wire.Error != "" {
		return wire.Error
	}
	if len(body) > 0 {
		return string(body)
	}
	return fallback
}

// fetchPinsDiff retrieves one server's diff from the running gateway.
func fetchPinsDiff(st *state.DaemonState, server string) (*pinsDiffServer, error) {
	url := fmt.Sprintf("http://localhost:%d/api/pins/%s/diff", st.Port, server)
	client := &http.Client{Timeout: pinsAPITimeout}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("calling pins API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	// 404 covers two distinct cases the daemon distinguishes in its message:
	// no pins for the server, or pins exist but the server is not in the
	// gateway (removed from the stack). Surface the daemon's reason verbatim.
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s (for a server removed from the stack, run 'gridctl pins reset %s')",
			apiErrorMessage(body, "no pins found for server "+server), server)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("diff failed: %s", string(body))
	}

	var wire struct {
		Server         string         `json:"server"`
		Status         string         `json:"status"`
		LiveServerHash string         `json:"live_server_hash"`
		ModifiedTools  []pinsToolDiff `json:"modified_tools"`
		NewTools       []string       `json:"new_tools"`
		RemovedTools   []string       `json:"removed_tools"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}
	return &pinsDiffServer{
		Name:           wire.Server,
		Status:         wire.Status,
		LiveServerHash: wire.LiveServerHash,
		ModifiedTools:  wire.ModifiedTools,
		NewTools:       wire.NewTools,
		RemovedTools:   wire.RemovedTools,
	}, nil
}

// hasChanges reports whether a server's diff carries any delta.
func (sv *pinsDiffServer) hasChanges() bool {
	return len(sv.ModifiedTools)+len(sv.NewTools)+len(sv.RemovedTools) > 0
}

// buildPinsDiffDoc assembles the diff report from the running gateway. With a
// named server it diffs that one server regardless of status. Without, it
// recomputes against live tools for every pinned server (the persisted status
// can be stale) and reports the ones with changes; servers that fail to diff
// are skipped with a warning rather than aborting the whole report.
func buildPinsDiffDoc(st *state.DaemonState, server string) (pinsDiffDoc, []string, error) {
	doc := pinsDiffDoc{
		SchemaVersion: pinsJSONSchemaVersion,
		Stack:         st.StackName,
		Servers:       []pinsDiffServer{},
	}

	if server != "" {
		sv, err := fetchPinsDiff(st, server)
		if err != nil {
			return doc, nil, err
		}
		doc.Servers = append(doc.Servers, *sv)
		doc.HasDrift = len(sv.ModifiedTools) > 0
		return doc, nil, nil
	}

	_, servers, err := loadPinsForCLI()
	if err != nil {
		return doc, nil, err
	}

	var warnings []string
	for _, name := range sortedMapKeys(servers) {
		sv, err := fetchPinsDiff(st, name)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skipping %s: %v", name, err))
			continue
		}
		if !sv.hasChanges() {
			continue
		}
		doc.Servers = append(doc.Servers, *sv)
		if len(sv.ModifiedTools) > 0 {
			doc.HasDrift = true
		}
	}
	return doc, warnings, nil
}

// pinsDiffExit renders the diff report and returns the process exit code:
// 0 no drift, 1 drift detected, 2 infrastructure error (including servers
// that could not be diffed, so CI never mistakes a partial report for clean).
// Drift outranks warnings: it is the actionable signal.
func pinsDiffExit(stdout, stderr io.Writer, doc pinsDiffDoc, warnings []string, format string) int {
	for _, wmsg := range warnings {
		fmt.Fprintln(stderr, "warning: "+wmsg)
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return pinsExitInfrastructure
		}
	} else {
		renderPinsDiffText(stdout, doc)
	}
	if doc.HasDrift {
		return pinsExitDrift
	}
	if len(warnings) > 0 {
		return pinsExitInfrastructure
	}
	return pinsExitOK
}

// renderPinsDiffText prints the human per-tool before/after view.
func renderPinsDiffText(w io.Writer, doc pinsDiffDoc) {
	if len(doc.Servers) == 0 {
		fmt.Fprintln(w, "No drift detected.")
		return
	}
	for i, sv := range doc.Servers {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s (%s)\n", sv.Name, sv.Status)
		if len(sv.ModifiedTools)+len(sv.NewTools)+len(sv.RemovedTools) == 0 {
			fmt.Fprintln(w, "  ✓ no changes")
			continue
		}
		for _, d := range sv.ModifiedTools {
			fmt.Fprintf(w, "  ~ %s\n", escapeNonPrintable(d.Name))
			fmt.Fprintf(w, "      old %s  %s\n", shortPinHash(d.OldHash), escapeNonPrintable(d.OldDescription))
			fmt.Fprintf(w, "      new %s  %s\n", shortPinHash(d.NewHash), escapeNonPrintable(d.NewDescription))
		}
		for _, name := range sv.NewTools {
			fmt.Fprintf(w, "  + %s (new tool, pinned on approve)\n", escapeNonPrintable(name))
		}
		for _, name := range sv.RemovedTools {
			fmt.Fprintf(w, "  - %s (removed from server)\n", escapeNonPrintable(name))
		}
	}
}

// shortPinHash abbreviates a pin hash for display, keeping any scheme prefix
// (e.g. "h2:") and the first 12 hex characters.
func shortPinHash(h string) string {
	prefix := ""
	if idx := strings.IndexByte(h, ':'); idx >= 0 {
		prefix, h = h[:idx+1], h[idx+1:]
	}
	if len(h) > 12 {
		h = h[:12]
	}
	return prefix + h
}

// escapeNonPrintable renders control and other non-printable runes as visible
// escape sequences such as \n or U+202E. Tool descriptions are instructions to
// the model, and poisoned ones hide payloads in exactly this channel, so the
// reviewer must see every character.
// Backslash is escaped too, so literal escape-looking text in a description
// cannot spoof the rendering of a real hidden character.
func escapeNonPrintable(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r == '\\' || !unicode.IsPrint(r) }) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\\' {
			b.WriteString(`\\`)
			continue
		}
		if unicode.IsPrint(r) {
			b.WriteRune(r)
			continue
		}
		q := strconv.QuoteRune(r)
		b.WriteString(q[1 : len(q)-1])
	}
	return b.String()
}

func runPinsApprove(server, expectHash string) error {
	st, err := resolveRunningStack()
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://localhost:%d/api/pins/%s/approve", st.Port, server)
	client := &http.Client{Timeout: pinsAPITimeout}

	var reqBody io.Reader
	if expectHash != "" {
		payload, err := json.Marshal(map[string]string{"expected_server_hash": expectHash})
		if err != nil {
			return fmt.Errorf("encoding request: %w", err)
		}
		reqBody = strings.NewReader(string(payload))
	}

	resp, err := client.Post(url, "application/json", reqBody)
	if err != nil {
		return fmt.Errorf("calling pins API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%s (for a server removed from the stack, run 'gridctl pins reset %s')",
			apiErrorMessage(body, "no pins found for server "+server), server)
	}
	if resp.StatusCode == http.StatusConflict {
		return fmt.Errorf("%s (run 'gridctl pins diff %s' to review the current definitions)",
			apiErrorMessage(body, "tool definitions changed since review"), server)
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
