package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/registry"

	"github.com/spf13/cobra"
)

const activateHTTPTimeout = 10 * time.Second

// Exit codes — matched against the conventions in cmd/gridctl/test.go and
// cmd/gridctl/optimize.go so CI scripts can rely on a stable contract.
const (
	activateExitOK             = 0
	activateExitNotFound       = 1
	activateExitInfrastructure = 2
)

var (
	activateStack  string
	activateFormat string
	activateQuiet  bool
)

var activateCmd = &cobra.Command{
	Use:   "activate <skill-name>",
	Short: "Promote a skill from draft to active",
	Long: `Promote a skill in the local registry from draft (or any other state)
to active by calling POST /api/registry/skills/{name}/activate on the
running daemon.

Default output is a one-line success message; use '--format json' to emit
{"skill":"<name>","state":"active"}, or '--quiet' to suppress the line
entirely.

Exit codes:
  0  skill activated
  1  skill not found
  2  infrastructure error (gateway unreachable, conflict, registry unavailable)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var err error
		if activateFormat, err = resolveFormat(activateFormat, cmd.Flags().Changed("format"), *activateJSON); err != nil {
			return err
		}
		port, err := resolveActivatePort(activateStack)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(activateExitInfrastructure)
		}

		baseURL := fmt.Sprintf("http://localhost:%d", port)
		exit := runActivate(os.Stdout, os.Stderr, baseURL, args[0], activateFormat, activateQuiet)
		if exit != activateExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

var activateJSON *bool

func init() {
	activateCmd.Flags().StringVarP(&activateStack, "stack", "s", "", "Stack to query (auto-detected when only one stack is running)")
	activateCmd.Flags().StringVar(&activateFormat, "format", "", "Output format: 'json' for machine-readable output")
	activateCmd.Flags().BoolVarP(&activateQuiet, "quiet", "q", false, "Suppress the human-readable success line")
	activateJSON = addJSONAlias(activateCmd)
}

// resolveActivatePort delegates to the shared running-port resolver with this
// command's error vocabulary.
func resolveActivatePort(stackName string) (int, error) {
	return resolveRunningPort("activate", stackName)
}

// runActivate posts to the activate endpoint and writes the appropriate
// output. Returns the process exit code so callers (tests and the cobra
// RunE) can decide how to surface it.
func runActivate(stdout, stderr io.Writer, baseURL, name, format string, quiet bool) int {
	sk, statusCode, body, err := postActivate(baseURL, name)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return activateExitInfrastructure
	}

	switch {
	case statusCode/100 == 2:
		renderActivateSuccess(stdout, name, sk, format, quiet)
		return activateExitOK
	case statusCode == http.StatusNotFound:
		fmt.Fprintf(stderr, "Skill not found: %s\n", name)
		return activateExitNotFound
	default:
		// 409, 503, 500, and anything else: surface the server's message
		// verbatim so the operator sees the pre-condition that failed.
		msg := extractServerError(body)
		if msg == "" {
			msg = fmt.Sprintf("activate: server returned %d %s", statusCode, http.StatusText(statusCode))
		}
		fmt.Fprintln(stderr, msg)
		return activateExitInfrastructure
	}
}

// postActivate sends POST /api/registry/skills/{name}/activate and
// returns the parsed skill on 2xx, plus the raw status and body so the
// caller can map non-success codes.
func postActivate(baseURL, name string) (*registry.AgentSkill, int, []byte, error) {
	target := fmt.Sprintf("%s/api/registry/skills/%s/activate", baseURL, url.PathEscape(name))
	client := &http.Client{Timeout: activateHTTPTimeout}
	resp, err := client.Post(target, "application/json", nil)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("activate: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, nil, fmt.Errorf("activate: reading response: %w", err)
	}

	if resp.StatusCode/100 != 2 {
		return nil, resp.StatusCode, body, nil
	}

	// Success. The handler currently returns 200 with the skill body;
	// tolerate 204 (and any empty 2xx body) so a future no-content
	// migration does not require a client change.
	if len(body) == 0 {
		return nil, resp.StatusCode, body, nil
	}
	var sk registry.AgentSkill
	if err := json.Unmarshal(body, &sk); err != nil {
		return nil, resp.StatusCode, body, fmt.Errorf("activate: parsing response: %w", err)
	}
	return &sk, resp.StatusCode, body, nil
}

// renderActivateSuccess prints the success line in the requested shape.
// JSON output is always emitted (Article X); --quiet suppresses only the
// human-readable default.
func renderActivateSuccess(w io.Writer, name string, sk *registry.AgentSkill, format string, quiet bool) {
	if strings.EqualFold(format, "json") {
		state := string(registry.StateActive)
		if sk != nil && sk.State != "" {
			state = string(sk.State)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"skill": name,
			"state": state,
		})
		return
	}
	if quiet {
		return
	}
	fmt.Fprintf(w, "Activated %s\n", name)
}

// extractServerError pulls the "error" field from the daemon's JSON
// error envelope; falls back to the raw body when the payload is not
// the expected shape.
func extractServerError(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != "" {
		return payload.Error
	}
	return trimmed
}
