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

	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

const testHTTPTimeout = 120 * time.Second

// Exit codes — matched against the convention in cmd/gridctl/optimize.go
// (0 pass, 1 actionable findings, 2 infrastructure failure). The runner
// inherits the contract directly so CI scripts can read either command
// the same way.
const (
	testExitOK             = 0
	testExitFailures       = 1
	testExitInfrastructure = 2
)

var (
	testStack     string
	testFormat    string
	testDryRun    bool
	testCriterion int
)

var testCmd = &cobra.Command{
	Use:   "test <skill-name>",
	Short: "Run a skill's acceptance_criteria against the registry",
	Long: `Evaluate the acceptance_criteria frontmatter on a registered skill.

The runner asks the configured LLM provider to judge each criterion
against the skill's body and prints a pass/fail report. Without a
provider (no ANTHROPIC_API_KEY in the vault), the runner falls back to
a deterministic adapter that reads explicit 'PASS:' / 'FAIL:' markers —
useful in CI when criteria are fixture-encoded.

Use '--dry-run' to list the criteria without evaluating them.
Use '--criterion <n>' to evaluate a single criterion by zero-based index.

Exit codes:
  0  every criterion passed
  1  at least one criterion failed
  2  infrastructure error (gateway unreachable, skill not found, etc.)`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		name := args[0]
		port, err := resolveTestPort(testStack)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(testExitInfrastructure)
		}

		report, err := fetchTestReport(port, name, testCriterion, testDryRun)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(testExitInfrastructure)
		}

		switch strings.ToLower(testFormat) {
		case "json":
			if err := renderTestJSON(os.Stdout, report); err != nil {
				return err
			}
		default:
			renderTestTable(os.Stdout, report)
		}

		if report.HasErrors() {
			os.Exit(testExitInfrastructure)
		}
		if report.HasFailures() {
			os.Exit(testExitFailures)
		}
		return nil
	},
}

func init() {
	testCmd.Flags().StringVarP(&testStack, "stack", "s", "", "Stack to query (auto-detected when only one stack is running)")
	testCmd.Flags().StringVar(&testFormat, "format", "", "Output format: 'json' for machine-readable output (default: table)")
	testCmd.Flags().BoolVar(&testDryRun, "dry-run", false, "List criteria without evaluating them")
	testCmd.Flags().IntVar(&testCriterion, "criterion", -1, "Zero-based index of a single criterion to evaluate (default: all)")
}

// resolveTestPort mirrors resolveOptimizePort. Kept separate so error
// messages name the right command.
func resolveTestPort(stackName string) (int, error) {
	states, err := state.List()
	if err != nil {
		return 0, fmt.Errorf("test: could not read state: %w", err)
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
		return 0, fmt.Errorf("test: stack %q is not running", stackName)
	}
	switch len(running) {
	case 0:
		return 0, fmt.Errorf("test: gateway not running; try `gridctl status` or `gridctl serve`")
	case 1:
		return running[0].Port, nil
	default:
		names := make([]string, len(running))
		for i, s := range running {
			names[i] = s.StackName
		}
		return 0, fmt.Errorf("test: multiple stacks running (%s); use --stack to pick one", strings.Join(names, ", "))
	}
}

// fetchTestReport calls POST /api/registry/skills/{name}/test on the
// local gateway and decodes the response. Non-2xx HTTP statuses become
// errors so the caller maps them to exit code 2.
func fetchTestReport(port int, name string, criterion int, dryRun bool) (registry.TestReport, error) {
	q := url.Values{}
	if criterion >= 0 {
		q.Set("criterion", strconv.Itoa(criterion))
	}
	if dryRun {
		q.Set("dry_run", "1")
	}
	target := fmt.Sprintf("http://localhost:%d/api/registry/skills/%s/test", port, url.PathEscape(name))
	if encoded := q.Encode(); encoded != "" {
		target += "?" + encoded
	}

	client := &http.Client{Timeout: testHTTPTimeout}
	resp, err := client.Post(target, "application/json", nil)
	if err != nil {
		return registry.TestReport{}, fmt.Errorf("test: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return registry.TestReport{}, fmt.Errorf("test: reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return registry.TestReport{}, fmt.Errorf("test: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var report registry.TestReport
	if err := json.Unmarshal(body, &report); err != nil {
		return registry.TestReport{}, fmt.Errorf("test: parsing response: %w", err)
	}
	return report, nil
}

// renderTestTable prints results as a styled table mirroring optimize's
// formatting. The footer summarises pass/fail/error counts so CI logs
// have a one-line outcome above the table.
func renderTestTable(w io.Writer, report registry.TestReport) {
	if len(report.Results) == 0 {
		fmt.Fprintf(w, "No criteria to run for skill %q.\n", report.SkillName)
		return
	}

	t := table.NewWriter()
	t.SetOutputMirror(w)
	t.SetStyle(table.StyleRounded)
	if report.DryRun {
		t.AppendHeader(table.Row{"#", "CRITERION"})
		for _, r := range report.Results {
			t.AppendRow(table.Row{r.Index, firstLine(r.Criterion)})
		}
	} else {
		t.AppendHeader(table.Row{"#", "VERDICT", "CRITERION", "RATIONALE"})
		for _, r := range report.Results {
			t.AppendRow(table.Row{
				r.Index,
				testVerdictLabel(r.Severity),
				firstLine(r.Criterion),
				firstLine(r.Message),
			})
		}
	}
	t.Render()

	fmt.Fprintln(w)
	if report.DryRun {
		fmt.Fprintf(w, "Skill: %s  ·  %d criteria  ·  dry run (no evaluation)\n", report.SkillName, len(report.Results))
		return
	}
	fmt.Fprintf(w, "Skill: %s  ·  pass: %d  ·  fail: %d  ·  error: %d  ·  evaluator: %s\n",
		report.SkillName, report.PassCount, report.FailCount, report.ErrorCount, report.Evaluator)
}

func renderTestJSON(w io.Writer, report registry.TestReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func testVerdictLabel(s registry.TestSeverity) string {
	switch s {
	case registry.TestSeverityPass:
		return "✓ pass"
	case registry.TestSeverityFail:
		return "✗ fail"
	case registry.TestSeverityError:
		return "! error"
	case "":
		return "—"
	default:
		return string(s)
	}
}
