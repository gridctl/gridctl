package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/spf13/cobra"
)

var testStack string
var testDryRun bool

var testCmd = &cobra.Command{
	Use:   "test <skill-name>",
	Short: "Run acceptance criteria tests for a skill",
	Long: `Execute acceptance criteria for a skill against the running gateway.

Exit codes:
  0  All criteria passed
  1  One or more criteria failed (or no acceptance criteria defined)
  2  Infrastructure error (gateway unreachable, skill not found)
  3  Criteria present but none parseable (all criteria were skipped)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTestCmd(args[0])
	},
}

func init() {
	testCmd.Flags().StringVarP(&testStack, "stack", "s", "", "Stack to test against (auto-detect if only one running)")
	testCmd.Flags().BoolVar(&testDryRun, "dry-run", false, "Show which criteria would run without executing against live tools")
}

func runTestCmd(skillName string) error {
	st, err := resolveRunningStack()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gateway not reachable: %v\n", err)
		os.Exit(2)
	}

	if testDryRun {
		return runTestDryRun(skillName, st.Port)
	}

	url := fmt.Sprintf("http://localhost:%d/api/registry/skills/%s/test", st.Port, skillName)
	client := &http.Client{Timeout: 5 * time.Minute}

	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "internal error: %v\n", err)
		os.Exit(2)
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gateway not reachable: %v\n", err)
		os.Exit(2)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading response: %v\n", err)
		os.Exit(2)
	}

	switch resp.StatusCode {
	case http.StatusNotFound:
		fmt.Fprintf(os.Stderr, "skill not found: %s\n", skillName)
		os.Exit(2)
	case http.StatusBadRequest:
		var errResp struct {
			Error string `json:"error"`
		}
		if jsonErr := json.Unmarshal(body, &errResp); jsonErr == nil {
			fmt.Fprintln(os.Stderr, errResp.Error)
		} else {
			fmt.Fprintf(os.Stderr, "%s\n", string(body))
		}
		os.Exit(1)
	case http.StatusServiceUnavailable:
		fmt.Fprintln(os.Stderr, "registry not available on this gateway")
		os.Exit(2)
	case http.StatusInternalServerError:
		fmt.Fprintf(os.Stderr, "gateway not reachable: %s\n", string(body))
		os.Exit(2)
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "unexpected status %d: %s\n", resp.StatusCode, string(body))
		os.Exit(2)
	}

	var result registry.SkillTestResult
	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Fprintf(os.Stderr, "parsing response: %v\n", err)
		os.Exit(2)
	}

	printTestResult(&result, st.Port)

	if result.Failed > 0 {
		os.Exit(1)
	}
	total := result.Passed + result.Failed + result.Skipped
	if total > 0 && result.Passed == 0 && result.Failed == 0 {
		os.Exit(3)
	}
	return nil
}

// testDisplayPattern extracts Given/When/Then for terminal display.
var testDisplayPattern = regexp.MustCompile(`(?i)GIVEN\s+(.+?)\s+WHEN\s+(.+?)\s+THEN\s+(.+)`)

func parseCriterionDisplay(criterion string) (given, when, then string) {
	m := testDisplayPattern.FindStringSubmatch(criterion)
	if m == nil {
		return criterion, "", ""
	}
	return strings.TrimSpace(m[1]), strings.TrimSpace(m[2]), strings.TrimSpace(m[3])
}

func printTestResult(result *registry.SkillTestResult, port int) {
	fmt.Printf("\nRunning acceptance criteria for skill: %s\n", result.Skill)
	fmt.Printf("Gateway: http://localhost:%d\n\n", port)

	for _, r := range result.Results {
		given, when, then := parseCriterionDisplay(r.Criterion)

		if r.Skipped {
			if when == "" {
				fmt.Printf("  ⚠ SKIPPED: %s\n", given)
			} else {
				fmt.Printf("  GIVEN %s\n", given)
				fmt.Printf("  WHEN  %s\n", when)
				fmt.Printf("  THEN  %s\n", then)
				fmt.Printf("  ⚠ skipped\n")
			}
			fmt.Printf("  → %s\n\n", r.SkipReason)
			continue
		}

		if when == "" {
			// Unparseable but not skipped — print raw
			fmt.Printf("  %s\n", r.Criterion)
		} else {
			fmt.Printf("  GIVEN %s\n", given)
			fmt.Printf("  WHEN  %s\n", when)
			fmt.Printf("  THEN  %s\n", then)
		}

		if r.Passed {
			fmt.Printf("  ✓ passed\n")
		} else {
			fmt.Printf("  ✗ failed\n")
			if r.Actual != "" {
				lines := strings.SplitN(r.Actual, "\n", 4)
				for _, l := range lines {
					fmt.Printf("  → %s\n", l)
				}
			}
		}
		fmt.Println()
	}

	total := result.Passed + result.Failed + result.Skipped
	fmt.Printf("%d criteria, %d passed, %d failed", total, result.Passed, result.Failed)
	if result.Skipped > 0 {
		fmt.Printf(", %d skipped", result.Skipped)
	}
	fmt.Println()
	fmt.Println()

	switch {
	case result.Failed > 0:
		fmt.Println("Skill status: FAILING")
	case result.Skipped == total:
		fmt.Println("Skill status: UNTESTED (no parseable criteria)")
	default:
		fmt.Println("Skill status: PASSING")
	}
}

// runTestDryRun fetches the skill definition and reports which criteria would
// parse without invoking any MCP tools.
func runTestDryRun(skillName string, port int) error {
	url := fmt.Sprintf("http://localhost:%d/api/registry/skills/%s", port, skillName)
	client := &http.Client{Timeout: 30 * time.Second}

	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gateway not reachable: %v\n", err)
		os.Exit(2)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		fmt.Fprintf(os.Stderr, "skill not found: %s\n", skillName)
		os.Exit(2)
	case http.StatusServiceUnavailable:
		fmt.Fprintln(os.Stderr, "registry not available on this gateway")
		os.Exit(2)
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "unexpected status %d\n", resp.StatusCode)
		os.Exit(2)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading response: %v\n", err)
		os.Exit(2)
	}
	var sk registry.AgentSkill
	if err := json.Unmarshal(body, &sk); err != nil {
		fmt.Fprintf(os.Stderr, "parsing response: %v\n", err)
		os.Exit(2)
	}

	printDryRunResult(os.Stdout, &sk, port)

	total := len(sk.AcceptanceCriteria)
	if total == 0 {
		os.Exit(1)
	}
	wouldRun := 0
	for _, c := range sk.AcceptanceCriteria {
		if registry.ParseCriterion(c) != nil {
			wouldRun++
		}
	}
	if wouldRun == 0 {
		os.Exit(3)
	}
	if wouldRun < total {
		os.Exit(1)
	}
	return nil
}

func printDryRunResult(w io.Writer, sk *registry.AgentSkill, port int) {
	fmt.Fprintf(w, "\nDry-run: acceptance criteria parse results for skill: %s\n", sk.Name)
	fmt.Fprintf(w, "Gateway: http://localhost:%d\n\n", port)

	wouldRun := 0
	wouldSkip := 0
	for _, criterion := range sk.AcceptanceCriteria {
		given, when, then := parseCriterionDisplay(criterion)
		if when == "" {
			fmt.Fprintf(w, "  %q\n", criterion)
			fmt.Fprintf(w, "  → would skip: does not match GIVEN ... WHEN ... THEN\n\n")
			wouldSkip++
		} else {
			fmt.Fprintf(w, "  GIVEN %s\n", given)
			fmt.Fprintf(w, "  WHEN  %s\n", when)
			fmt.Fprintf(w, "  THEN  %s\n", then)
			toolName := resolveToolNameDisplay(when, sk.Name)
			fmt.Fprintf(w, "  → would run (tool: %s)\n\n", toolName)
			wouldRun++
		}
	}

	total := wouldRun + wouldSkip
	fmt.Fprintf(w, "%d of %d criteria would run", wouldRun, total)
	if wouldSkip > 0 {
		fmt.Fprintf(w, ", %d would be skipped", wouldSkip)
	}
	fmt.Fprintln(w)
	if wouldSkip > 0 || total == 0 {
		fmt.Fprintln(w, "Run without --dry-run to execute against live tools.")
	}
	fmt.Fprintln(w)
}

// resolveToolNameDisplay returns the tool name that would be called for the WHEN clause.
// Mirrors resolveToolName() in pkg/registry/tester.go for dry-run display.
func resolveToolNameDisplay(when, skillName string) string {
	lower := strings.ToLower(strings.TrimSpace(when))
	if strings.Contains(lower, "the skill is called") {
		return skillName
	}
	if idx := strings.LastIndex(lower, " is called"); idx != -1 {
		candidate := strings.TrimSpace(when[:idx])
		if candidate != "" {
			return candidate
		}
	}
	for _, f := range strings.Fields(when) {
		clean := strings.Trim(f, ".,;:()")
		if strings.Contains(clean, "__") {
			return clean
		}
	}
	return skillName
}
