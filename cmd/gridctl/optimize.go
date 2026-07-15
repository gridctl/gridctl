package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/optimize"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

const optimizeHTTPTimeout = 10 * time.Second

// Exit codes — matched against the conventions in cmd/gridctl/pins.go
// and cmd/gridctl/validate.go so CI scripts can rely on a stable
// contract.
const (
	optimizeExitOK             = 0
	optimizeExitFindings       = 1
	optimizeExitInfrastructure = 2
)

var (
	optimizeStack     string
	optimizeMinImpact float64
	optimizeSeverity  string
	optimizeFormat    string
)

var optimizeCmd = &cobra.Command{
	Use:   "optimize",
	Short: "Surface cost-reduction findings from gateway-observed data",
	Long: `Analyze the running gateway for unused servers and tools and print
actionable findings with weekly USD impact.

Default output is a styled table; use '--format json' to emit the same
OptimizeReport the API returns.

Exit codes:
  0  no findings, or only info-level findings
  1  at least one warn or critical finding
  2  infrastructure error (gateway unreachable, wrong stack)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var err error
		if optimizeFormat, err = resolveFormat(optimizeFormat, cmd.Flags().Changed("format"), *optimizeJSON); err != nil {
			return err
		}
		if err := resolvePlain(*optimizePlain, optimizeFormat); err != nil {
			return err
		}
		port, err := resolveOptimizePort(optimizeStack)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(optimizeExitInfrastructure)
		}

		report, err := fetchOptimizeReport(port, optimizeStack, optimizeMinImpact, optimizeSeverity)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(optimizeExitInfrastructure)
		}

		switch strings.ToLower(optimizeFormat) {
		case "json":
			if err := renderOptimizeJSON(os.Stdout, report); err != nil {
				return err
			}
		default:
			renderOptimizeTable(os.Stdout, report, *optimizePlain)
		}

		if hasActionableFindings(report.Findings) {
			os.Exit(optimizeExitFindings)
		}
		return nil
	},
}

func init() {
	optimizeCmd.Flags().StringVarP(&optimizeStack, "stack", "s", "", "Stack to query (auto-detected when only one stack is running)")
	optimizeCmd.Flags().Float64Var(&optimizeMinImpact, "min-impact", 0, "Filter findings below this weekly USD impact (info findings always shown)")
	optimizeCmd.Flags().StringVar(&optimizeSeverity, "severity", "", "Comma-separated severity allowlist: info,warn,critical")
	optimizeCmd.Flags().StringVar(&optimizeFormat, "format", "", "Output format: 'json' for machine-readable output (default: table)")
	optimizeJSON = addJSONAlias(optimizeCmd)
	optimizePlain = addPlainFlag(optimizeCmd)
}

var (
	optimizeJSON  *bool
	optimizePlain *bool
)

// resolveOptimizePort finds the port of a running gateway, optionally
// filtered by stack name. Mirrors resolveTracesPort, but emits errors
// matching the optimize command's vocabulary.
func resolveOptimizePort(stackName string) (int, error) {
	states, err := state.List()
	if err != nil {
		return 0, fmt.Errorf("optimize: could not read state: %w", err)
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
		return 0, fmt.Errorf("optimize: stack %q is not running", stackName)
	}
	switch len(running) {
	case 0:
		return 0, fmt.Errorf("optimize: gateway not running; try `gridctl status`")
	case 1:
		return running[0].Port, nil
	default:
		names := make([]string, len(running))
		for i, s := range running {
			names[i] = s.StackName
		}
		return 0, fmt.Errorf("optimize: multiple stacks running (%s); use --stack to pick one", strings.Join(names, ", "))
	}
}

// fetchOptimizeReport calls GET /api/optimize on the local gateway and
// decodes the response. Non-2xx HTTP statuses are mapped to errors so
// the caller can map them to exit code 2.
func fetchOptimizeReport(port int, stack string, minImpact float64, severity string) (optimize.OptimizeReport, error) {
	q := url.Values{}
	if stack != "" {
		q.Set("stack", stack)
	}
	if minImpact > 0 {
		q.Set("min_impact", strconv.FormatFloat(minImpact, 'f', -1, 64))
	}
	if severity != "" {
		q.Set("severity", severity)
	}
	target := fmt.Sprintf("http://localhost:%d/api/optimize", port)
	if encoded := q.Encode(); encoded != "" {
		target += "?" + encoded
	}

	client := &http.Client{Timeout: optimizeHTTPTimeout}
	resp, err := client.Get(target)
	if err != nil {
		return optimize.OptimizeReport{}, fmt.Errorf("optimize: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return optimize.OptimizeReport{}, fmt.Errorf("optimize: reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return optimize.OptimizeReport{}, fmt.Errorf("optimize: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var report optimize.OptimizeReport
	if err := json.Unmarshal(body, &report); err != nil {
		return optimize.OptimizeReport{}, fmt.Errorf("optimize: parsing response: %w", err)
	}
	return report, nil
}

// renderOptimizeTable prints findings as a styled table mirroring the
// pins-list / traces-list formatting. An empty report still prints the
// health-score footer so users see "100" on a clean stack.
func renderOptimizeTable(w io.Writer, report optimize.OptimizeReport, plain bool) {
	if len(report.Findings) == 0 {
		fmt.Fprintf(w, "No findings. Health score: %d/100\n", report.HealthScore)
		return
	}

	t := output.NewTableWriter(w, plain)
	t.AppendHeader(table.Row{"SEVERITY", "TITLE", "WEEKLY $", "REMEDIATION"})

	for _, f := range report.Findings {
		t.AppendRow(table.Row{
			severityLabel(f.Severity),
			f.Title,
			formatImpact(f.ImpactUSDPerWeek),
			firstLine(f.Remediation),
		})
	}
	t.Render()

	fmt.Fprintf(w, "\nHealth score: %d/100  ·  %d finding(s)\n", report.HealthScore, len(report.Findings))
	for _, f := range report.Findings {
		if f.Severity == optimize.SeverityInfo && f.Heuristic == "need_more_data" {
			continue
		}
		fmt.Fprintln(w)
		fmt.Fprintf(w, "## %s\n%s\n\n", f.Title, f.Summary)
		if f.Remediation != "" {
			fmt.Fprintln(w, f.Remediation)
		}
	}
}

func renderOptimizeJSON(w io.Writer, report optimize.OptimizeReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func hasActionableFindings(findings []optimize.Finding) bool {
	for _, f := range findings {
		if f.Severity.IsActionable() {
			return true
		}
	}
	return false
}

func severityLabel(s optimize.Severity) string {
	switch s {
	case optimize.SeverityCritical:
		return "✗ critical"
	case optimize.SeverityWarn:
		return "⚠ warn"
	case optimize.SeverityInfo:
		return "ℹ info"
	default:
		return string(s)
	}
}

func formatImpact(usd float64) string {
	if usd <= 0 {
		return "—"
	}
	if usd < 0.01 {
		return "<$0.01"
	}
	return fmt.Sprintf("$%.2f", usd)
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
