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

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

const limitsHTTPTimeout = 10 * time.Second

// Exit codes — matched against the optimize/pins conventions so CI scripts
// can rely on a stable contract.
const (
	limitsExitOK             = 0
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
	Short: "Show rate limit state",
	Long: `Show every configured token-bucket rate limit with its current
state.

Limits are declared in stack.yaml under 'limits:' and enforced at tool-call
dispatch.

Default output is a styled table; use '--format json' to emit the same
status report the API returns.

Exit codes:
  0  success (including no limits configured)
  2  infrastructure error (gateway unreachable)`,
	Example: `  gridctl limits              Show the state of every rate limit
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
		return nil
	},
}

func init() {
	limitsCmd.Flags().StringVarP(&limitsStack, "stack", "s", "", "Stack to query (auto-detected when only one stack is running)")
	limitsCmd.Flags().StringVar(&limitsFormat, "format", "", "Output format: 'json' for machine-readable output (default: table)")
	limitsJSON = addJSONAlias(limitsCmd)
	limitsPlain = addPlainFlag(limitsCmd)
}

// resolveLimitsPort delegates to the shared running-port resolver with this
// command's error vocabulary.
func resolveLimitsPort(stackName string) (int, error) {
	return resolveRunningPort("limits", stackName)
}

// fetchLimitsReport calls GET /api/limits on the local gateway.
func fetchLimitsReport(port int) (limits.StatusReport, error) {
	api := newDaemonAPI(port, limitsHTTPTimeout)
	resp, err := api.Get(api.URL("/api/limits"))
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


// renderLimitsTable prints the state table, or a configuration hint when no
// limits block exists.
func renderLimitsTable(w io.Writer, report limits.StatusReport, plain bool) {
	if !report.Configured {
		fmt.Fprintln(w, "No limits configured. Add a 'limits:' block to stack.yaml, e.g.:")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "  limits:")
		fmt.Fprintln(w, "    rate_limits:")
		fmt.Fprintln(w, "      - server: github")
		fmt.Fprintln(w, "        calls_per_minute: 30")
		return
	}

	t := output.NewTableWriter(w, plain)
	t.AppendHeader(table.Row{"Kind", "Scope", "Key", "Limit", "Burst", "State"})
	for _, e := range report.Entries {
		var limit, burst string
		if e.Rate != nil {
			limit = fmt.Sprintf("%d calls/min", e.Rate.CallsPerMinute)
			burst = fmt.Sprintf("%d", e.Rate.Burst)
		}
		t.AppendRow(table.Row{e.Kind, e.Scope, e.Key, limit, burst, e.State})
	}
	t.Render()
}
