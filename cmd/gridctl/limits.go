package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/limits"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

const limitsHTTPTimeout = 10 * time.Second

// Exit codes — matched against the optimize/pins conventions so CI scripts
// can rely on a stable contract.
const (
	limitsExitOK             = 0
	limitsExitExceeded       = 1
	limitsExitInfrastructure = 2
)

var (
	limitsStack  string
	limitsFormat string
	limitsJSON   *bool
	limitsPlain  *bool
)

var limitsCmd = &cobra.Command{
	Use:   "limits",
	Short: "Show budget and rate limit consumption",
	Long: `Show every configured budget and rate limit with its current
consumption: spend against dollar caps (with the active calendar window)
and token-bucket rate limits.

Limits are declared in stack.yaml under 'limits:' and enforced at tool-call
dispatch. Budgets govern attributed cost only; calls whose model cannot be
priced spend outside every budget's sight, so pair budgets with rate limits
as a backstop.

Default output is a styled table; use '--format json' to emit the same
status report the API returns.

Exit codes:
  0  all limits clear (or no limits configured)
  1  at least one budget exceeded
  2  infrastructure error (gateway unreachable)`,
	Example: `  gridctl limits              Show consumption against every limit
  gridctl limits --json       Machine-readable status
  gridctl limits -s my-stack  Query a specific running stack`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var err error
		if limitsFormat, err = resolveFormat(limitsFormat, cmd.Flags().Changed("format"), *limitsJSON); err != nil {
			return err
		}
		if err := resolvePlain(*limitsPlain, limitsFormat); err != nil {
			return err
		}
		port, err := resolveLimitsPort(limitsStack)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(limitsExitInfrastructure)
		}

		report, err := fetchLimitsReport(port)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(limitsExitInfrastructure)
		}

		if strings.EqualFold(limitsFormat, "json") {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(report); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(limitsExitInfrastructure)
			}
		} else {
			renderLimitsTable(os.Stdout, report, *limitsPlain)
		}

		if limitsExceeded(report) {
			os.Exit(limitsExitExceeded)
		}
		return nil
	},
}

func init() {
	limitsCmd.Flags().StringVarP(&limitsStack, "stack", "s", "", "Stack to query (auto-detected when only one stack is running)")
	limitsCmd.Flags().StringVar(&limitsFormat, "format", "", "Output format: 'json' for machine-readable output (default: table)")
	limitsJSON = addJSONAlias(limitsCmd)
	limitsPlain = addPlainFlag(limitsCmd)
}

// resolveLimitsPort finds the port of a running gateway, optionally filtered
// by stack name. Mirrors resolveOptimizePort with this command's vocabulary.
func resolveLimitsPort(stackName string) (int, error) {
	states, err := state.List()
	if err != nil {
		return 0, fmt.Errorf("limits: could not read state: %w", err)
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
		return 0, fmt.Errorf("limits: stack %q is not running", stackName)
	}
	switch len(running) {
	case 0:
		return 0, fmt.Errorf("limits: gateway not running; try `gridctl status`")
	case 1:
		return running[0].Port, nil
	default:
		names := make([]string, len(running))
		for i, s := range running {
			names[i] = s.StackName
		}
		return 0, fmt.Errorf("limits: multiple stacks running (%s); use --stack to pick one", strings.Join(names, ", "))
	}
}

// fetchLimitsReport calls GET /api/limits on the local gateway.
func fetchLimitsReport(port int) (limits.StatusReport, error) {
	client := &http.Client{Timeout: limitsHTTPTimeout}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/api/limits", port))
	if err != nil {
		return limits.StatusReport{}, fmt.Errorf("limits: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return limits.StatusReport{}, fmt.Errorf("limits: reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return limits.StatusReport{}, fmt.Errorf("limits: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var report limits.StatusReport
	if err := json.Unmarshal(body, &report); err != nil {
		return limits.StatusReport{}, fmt.Errorf("limits: parsing response: %w", err)
	}
	return report, nil
}

// limitsExceeded reports whether any budget entry is over its cap. Rate
// entries do not affect the exit code: a momentarily drained bucket is
// normal operation, not an actionable condition.
func limitsExceeded(report limits.StatusReport) bool {
	for _, e := range report.Entries {
		if e.Kind == "budget" && e.State == "exceeded" {
			return true
		}
	}
	return false
}

// renderLimitsTable prints the consumption table, or a configuration hint
// when no limits block exists.
func renderLimitsTable(w io.Writer, report limits.StatusReport, plain bool) {
	if !report.Configured {
		fmt.Fprintln(w, "No limits configured. Add a 'limits:' block to stack.yaml, e.g.:")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "  limits:")
		fmt.Fprintln(w, "    budgets:")
		fmt.Fprintln(w, "      - client: claude-code")
		fmt.Fprintln(w, "        max_usd: 5.00")
		fmt.Fprintln(w, "        period: daily")
		fmt.Fprintln(w, "    rate_limits:")
		fmt.Fprintln(w, "      - server: github")
		fmt.Fprintln(w, "        calls_per_minute: 30")
		return
	}

	t := output.NewTableWriter(w, plain)
	t.AppendHeader(table.Row{"Kind", "Scope", "Key", "Limit", "Used", "Window", "State"})
	for _, e := range report.Entries {
		var limit, used, window string
		switch {
		case e.Budget != nil:
			limit = fmt.Sprintf("$%.2f %s", e.Budget.MaxUSD, e.Budget.Period)
			used = fmt.Sprintf("$%.2f (%.0f%%)", e.Budget.SpentUSD, e.Budget.Percent)
			window = "resets " + e.Budget.WindowEnd.Format("2006-01-02 15:04")
		case e.Rate != nil:
			limit = fmt.Sprintf("%d calls/min", e.Rate.CallsPerMinute)
			used = fmt.Sprintf("burst %d", e.Rate.Burst)
		}
		t.AppendRow(table.Row{e.Kind, e.Scope, e.Key, limit, used, window, e.State})
	}
	t.Render()
}
