package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/agent/persist"
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

const runsAPITimeout = 15 * time.Second

var (
	runsFormat      string
	runsListLimit   int
	runsResumeFrom  string
	runsApproveDec  string
	runsApproveText string
	runsStackName   string
)

var runsCmd = &cobra.Command{
	Use:   "runs",
	Short: "Inspect and resume agent runtime runs",
	Long: `Manage runs of typed Skills executed against the agent runtime.

Run state is persisted as JSONL at ~/.gridctl/runs/<run_id>.jsonl. Use the
sub-commands below to list, inspect, resume, and respond to approval gates.`,
}

var runsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recorded runs (newest first)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRunsList()
	},
}

var runsInspectCmd = &cobra.Command{
	Use:   "inspect <run_id>",
	Short: "Show the typed event timeline for a run",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRunsInspect(args[0])
	},
}

var runsTraceCmd = &cobra.Command{
	Use:   "trace <run_id>",
	Short: "Render a run's events as OTel-shaped JSON",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRunsTrace(args[0])
	},
}

var runsResumeCmd = &cobra.Command{
	Use:   "resume <run_id>",
	Short: "Resume a suspended run from the last checkpoint",
	Long: `Rehydrates run state from JSONL and re-issues a resume request to the daemon.

The runtime hook that re-executes graphs lands alongside the agent IDE; today
this command surfaces the resume plan and records a resume marker in the
ledger so the inspect view shows the boundary clearly.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRunsResume(args[0])
	},
}

var runsApproveCmd = &cobra.Command{
	Use:   "approve <run_id>",
	Short: "Respond to an approval gate",
	Long: `Sends an approval decision for a run that is suspended on an approval gate.

The decision is recorded in the ledger and (when the daemon is running) wakes
the gate that the skill is blocked on. Use --decision approve|reject and
--reason "<text>" to supply the verdict and rationale.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRunsApprove(args[0])
	},
}

func init() {
	runsCmd.PersistentFlags().StringVar(&runsFormat, "format", "text", "Output format: text, json")
	runsCmd.PersistentFlags().StringVar(&runsStackName, "stack", "", "Stack to address for resume/approve API calls (auto-detected when only one stack is running)")

	runsListCmd.Flags().IntVar(&runsListLimit, "limit", 50, "Maximum number of runs to list")

	runsResumeCmd.Flags().StringVar(&runsResumeFrom, "from-step", "", "Resume from the given node ID (defaults to the last completed step)")

	runsApproveCmd.Flags().StringVar(&runsApproveDec, "decision", "approve", "Decision: approve, reject")
	runsApproveCmd.Flags().StringVar(&runsApproveText, "reason", "", "Optional rationale recorded with the decision")

	runsCmd.AddCommand(runsListCmd)
	runsCmd.AddCommand(runsInspectCmd)
	runsCmd.AddCommand(runsTraceCmd)
	runsCmd.AddCommand(runsResumeCmd)
	runsCmd.AddCommand(runsApproveCmd)
}

// runsStore returns a persist.Store anchored at the default runs
// directory. Callers may override the directory through the
// GRIDCTL_RUNS_DIR environment variable; the override is intended for
// CI and integration tests that don't want to touch $HOME.
func runsStore() *persist.Store {
	if dir := os.Getenv("GRIDCTL_RUNS_DIR"); dir != "" {
		return persist.NewStore(dir)
	}
	return persist.NewStore(persist.DefaultRunsDir())
}

func runRunsList() error {
	store := runsStore()
	summaries, err := store.List(runsListLimit)
	if err != nil {
		return fmt.Errorf("listing runs: %w", err)
	}
	if runsFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(summaries)
	}
	if len(summaries) == 0 {
		fmt.Printf("No runs found in %s.\n", store.Dir())
		return nil
	}
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleRounded)
	t.AppendHeader(table.Row{"RUN_ID", "SKILL", "FLAVOR", "STATUS", "EVENTS", "STARTED"})
	for _, s := range summaries {
		started := "—"
		if !s.StartedAt.IsZero() {
			started = s.StartedAt.Local().Format("2006-01-02 15:04:05")
		}
		t.AppendRow(table.Row{
			s.RunID,
			fallback(s.Skill, "—"),
			fallback(s.Flavor, "—"),
			runStatusLabel(s),
			s.EventCount,
			started,
		})
	}
	t.Render()
	return nil
}

func runRunsInspect(runID string) error {
	store := runsStore()
	events, err := store.Read(runID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("run %q not found in %s", runID, store.Dir())
		}
		return fmt.Errorf("reading run: %w", err)
	}
	if runsFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(events)
	}
	if len(events) == 0 {
		fmt.Printf("Run %s has no events.\n", runID)
		return nil
	}
	for _, ev := range events {
		printEventLine(ev)
	}
	return nil
}

func runRunsTrace(runID string) error {
	store := runsStore()
	events, err := store.Read(runID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("run %q not found in %s", runID, store.Dir())
		}
		return fmt.Errorf("reading run: %w", err)
	}
	spans := otelShape(runID, events)
	out, err := json.MarshalIndent(spans, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

func runRunsResume(runID string) error {
	store := runsStore()
	plan, err := store.BuildResumePlan(runID, runsResumeFrom)
	if err != nil {
		return err
	}
	if plan.PendingApproval != "" {
		fmt.Fprintf(os.Stderr, "Run %s is suspended on approval %s. Use `gridctl runs approve %s` instead.\n", runID, plan.PendingApproval, runID)
	}

	// Best-effort: ping the daemon to drive the resume hook. If no
	// daemon is running, the local plan is still recorded in the
	// ledger so the next daemon boot can pick it up.
	if st, err := resolveOptionalRunningStack(); err == nil && st != nil {
		body, _ := json.Marshal(map[string]string{"from_step": runsResumeFrom})
		url := fmt.Sprintf("http://localhost:%d/api/agent/runs/%s/resume", st.Port, runID)
		client := &http.Client{Timeout: runsAPITimeout}
		resp, postErr := client.Post(url, "application/json", strings.NewReader(string(body)))
		if postErr == nil {
			defer resp.Body.Close()
			io.Copy(io.Discard, resp.Body) //nolint:errcheck // best-effort drain
		}
	}

	if runsFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(plan)
	}
	fmt.Printf("Resume plan for %s\n", runID)
	if plan.Started != nil && plan.Started.Skill != "" {
		fmt.Printf("  Skill:     %s\n", plan.Started.Skill)
	}
	target := plan.FromNodeID
	if target == "" {
		target = "(last completed step)"
	}
	fmt.Printf("  From:      %s\n", target)
	fmt.Printf("  Replay:    %d events (last seq=%d)\n", len(plan.Replay), plan.LastSeq)
	if plan.PendingApproval != "" {
		fmt.Printf("  Pending:   approval %s — use `gridctl runs approve` instead\n", plan.PendingApproval)
	}
	return nil
}

func runRunsApprove(runID string) error {
	approved, err := parseApprovalDecision(runsApproveDec)
	if err != nil {
		return err
	}

	st, err := resolveRunningStack()
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://localhost:%d/api/agent/runs/%s/approve", st.Port, runID)
	body, _ := json.Marshal(map[string]any{
		"approved": approved,
		"reason":   runsApproveText,
		"source":   "cli",
	})
	client := &http.Client{Timeout: runsAPITimeout}
	resp, err := client.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("calling approve API: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("run %q has no pending approval (or was already resolved)", runID)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("approve failed: %s", string(respBody))
	}
	if runsFormat == "json" {
		fmt.Println(string(respBody))
		return nil
	}
	if approved {
		fmt.Printf("✓ Approved run %s\n", runID)
	} else {
		fmt.Printf("✗ Rejected run %s\n", runID)
	}
	return nil
}

// resolveOptionalRunningStack mirrors resolveRunningStack but returns
// (nil, nil) when no stack is running. Used by `runs resume` so the
// command keeps working in a stackless environment.
func resolveOptionalRunningStack() (*state.DaemonState, error) {
	st, err := resolveRunningStack()
	if err != nil {
		return nil, nil //nolint:nilerr // optional resolution
	}
	return st, nil
}

func parseApprovalDecision(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "approve", "approved", "yes", "y", "ok":
		return true, nil
	case "reject", "rejected", "no", "n", "deny":
		return false, nil
	default:
		return false, fmt.Errorf("invalid --decision %q (must be approve or reject)", raw)
	}
}

// printEventLine renders one event in the human-readable inspect
// format. Body is rendered compactly so a 30-event run stays on one
// screen; full payloads are accessible via `runs inspect --format json`.
func printEventLine(ev persist.Event) {
	ts := ev.Time.Local().Format("15:04:05.000")
	fmt.Printf("%-12s [%04d] %-22s ", ts, ev.Seq, ev.Type)
	switch ev.Type {
	case persist.EventRunStarted:
		var p persist.RunStartedPayload
		_ = json.Unmarshal(ev.Payload, &p)
		fmt.Printf("skill=%s flavor=%s\n", fallback(p.Skill, "—"), fallback(p.Flavor, "—"))
	case persist.EventRunCompleted:
		var p persist.RunCompletedPayload
		_ = json.Unmarshal(ev.Payload, &p)
		fmt.Printf("status=%s\n", p.Status)
	case persist.EventNodeEnter:
		var p persist.NodeEnterPayload
		_ = json.Unmarshal(ev.Payload, &p)
		fmt.Printf("node=%s\n", fallback(p.NodeName, p.NodeID))
	case persist.EventNodeExit:
		var p persist.NodeExitPayload
		_ = json.Unmarshal(ev.Payload, &p)
		flag := "ok"
		if !p.Success {
			flag = "err"
		}
		fmt.Printf("node=%s %s in %dµs\n", p.NodeID, flag, p.DurationMicros)
	case persist.EventToolCall:
		var p persist.ToolCallPayload
		_ = json.Unmarshal(ev.Payload, &p)
		fmt.Printf("tool=%s call=%s\n", p.Name, p.CallID)
	case persist.EventToolResult:
		var p persist.ToolResultPayload
		_ = json.Unmarshal(ev.Payload, &p)
		flag := "ok"
		if p.IsError {
			flag = "err"
		}
		fmt.Printf("call=%s %s\n", p.CallID, flag)
	case persist.EventLLMCall:
		var p persist.LLMCallPayload
		_ = json.Unmarshal(ev.Payload, &p)
		fmt.Printf("model=%s tokens=%d/%d cost=$%.4f\n", p.Model, p.PromptTokens, p.OutputTokens, p.CostUSD)
	case persist.EventApprovalRequest:
		var p persist.ApprovalRequestPayload
		_ = json.Unmarshal(ev.Payload, &p)
		fmt.Printf("approval_id=%s timeout=%ds prompt=%q\n", p.ApprovalID, p.TimeoutSeconds, p.Prompt)
	case persist.EventApprovalResponse:
		var p persist.ApprovalResponsePayload
		_ = json.Unmarshal(ev.Payload, &p)
		dec := "rejected"
		if p.Approved {
			dec = "approved"
		}
		fmt.Printf("approval_id=%s %s via %s\n", p.ApprovalID, dec, fallback(p.Source, "?"))
	case persist.EventError:
		var p persist.ErrorPayload
		_ = json.Unmarshal(ev.Payload, &p)
		fmt.Printf("err=%q\n", p.Message)
	default:
		fmt.Println()
	}
}

// otelShape projects a JSONL ledger into a simple OTel-flavored span
// list. The shape is intentionally lightweight — production tracing
// flows through pkg/tracing — but the CLI affordance gives operators
// a copy-pasteable JSON when an OTel pipeline isn't configured.
func otelShape(runID string, events []persist.Event) any {
	type span struct {
		TraceID    string         `json:"trace_id,omitempty"`
		SpanID     string         `json:"span_id,omitempty"`
		Name       string         `json:"name"`
		Kind       string         `json:"kind"`
		StartTime  time.Time      `json:"start_time"`
		EndTime    time.Time      `json:"end_time,omitempty"`
		Attributes map[string]any `json:"attributes,omitempty"`
	}
	var traceID string
	open := make(map[string]int)
	spans := make([]span, 0, len(events))
	for _, ev := range events {
		switch ev.Type {
		case persist.EventRunStarted:
			var p persist.RunStartedPayload
			_ = json.Unmarshal(ev.Payload, &p)
			traceID = p.TraceID
		case persist.EventNodeEnter:
			var p persist.NodeEnterPayload
			_ = json.Unmarshal(ev.Payload, &p)
			spans = append(spans, span{
				TraceID:    traceID,
				SpanID:     p.SpanID,
				Name:       fallback(p.NodeName, p.NodeID),
				Kind:       "node",
				StartTime:  ev.Time,
				Attributes: map[string]any{"run_id": runID, "node_id": p.NodeID},
			})
			open[p.NodeID] = len(spans) - 1
		case persist.EventNodeExit:
			var p persist.NodeExitPayload
			_ = json.Unmarshal(ev.Payload, &p)
			if idx, ok := open[p.NodeID]; ok {
				spans[idx].EndTime = ev.Time
				if spans[idx].Attributes == nil {
					spans[idx].Attributes = map[string]any{}
				}
				spans[idx].Attributes["success"] = p.Success
				spans[idx].Attributes["duration_micros"] = p.DurationMicros
				delete(open, p.NodeID)
			}
		}
	}
	return map[string]any{"run_id": runID, "trace_id": traceID, "spans": spans}
}

func runStatusLabel(s persist.RunSummary) string {
	switch s.Status {
	case "ok":
		return "✓ ok"
	case "error":
		return "✗ error"
	case "cancelled":
		return "— cancelled"
	case "awaiting_approval":
		if s.PendingApproval != "" {
			return "⏸ approval"
		}
		return "⏸ waiting"
	case "running":
		return "» running"
	default:
		return s.Status
	}
}

// fallback returns def when v is empty.
func fallback(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// init: register runsCmd with the root command.
func init() {
	rootCmd.AddCommand(runsCmd)
}
