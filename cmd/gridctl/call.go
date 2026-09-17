package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/runs"
	"github.com/spf13/cobra"
)

var (
	callStack   string
	callAs      string
	callTimeout time.Duration
	callFormat  string
	callJSON    *bool
	callMatch   string
	callLimit   int
)

var callCmd = &cobra.Command{
	Use:   "call <server__tool> [json|@file]",
	Short: "Invoke a live gateway tool",
	Long: `Invoke one canonical tool on a running gateway.

The target is server__tool. Omitted arguments are {}. Inline JSON may appear
in shell history and process arguments; @file.json reads a local object
instead and does not encrypt it. No URL, stdin, or environment interpolation
is performed.

--as sets the caller-declared scope and accounting label. It can select a
different configured profile and does not authenticate as that client. A
denied cli call does not fall back to another profile.

Matching in live help uses substring search, not semantic intent. Queries
also match the generated description prefix:
MCP server: <server>. Call using the exact tool name "<canonical-name>".
Queries such as mcp, server, and call therefore match every scoped tool in
the selected server subset, if any, before limits.`,
	Example: `  gridctl call echo__echo '{"message":"hello"}' --format json
  gridctl call echo__echo @args.json --stack demo --timeout 30s
  gridctl call echo --help --match message --limit 20
  gridctl call echo__echo --help --format json`,
	Args: cobra.MaximumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCall(cmd, args)
	},
}

func init() {
	callCmd.Flags().StringVar(&callStack, "stack", "", "Stack name when more than one gateway is running")
	callCmd.Flags().StringVar(&callAs, "as", "", "Caller-declared scope/accounting label (does not authenticate)")
	callCmd.Flags().DurationVar(&callTimeout, "timeout", 60*time.Second, "Request timeout (must be positive)")
	callCmd.Flags().StringVar(&callFormat, "format", "", "Output format (json)")
	callJSON = addJSONAlias(callCmd)
	callCmd.Flags().StringVar(&callMatch, "match", "", "Substring filter for server help")
	callCmd.Flags().IntVar(&callLimit, "limit", 20, "Maximum tools for server help (1-200)")
}

func runCall(cmd *cobra.Command, args []string) error {
	asJSON, err := callJSONMode(cmd)
	if err != nil {
		return invalidCallErr(asJSON, "", "", "input", reasonInvalidFlag, err.Error())
	}
	if cmd.Flags().Changed("match") || cmd.Flags().Changed("limit") {
		return invalidCallErr(asJSON, "", "", "input", reasonInvalidFlag, "--match and --limit are only valid with targeted server help")
	}
	if callTimeout <= 0 {
		return invalidCallErr(asJSON, "", "", "input", reasonInvalidTimeout, "timeout must be a positive duration")
	}
	if len(args) == 0 {
		return invalidCallErr(asJSON, "", "", "input", reasonInvalidTarget, "target must be server__tool")
	}
	name, err := parseCallTarget(args[0])
	if err != nil {
		return invalidCallErr(asJSON, "", "", "input", reasonInvalidTarget, "target must be server__tool")
	}
	src := ""
	if len(args) > 1 {
		src = args[1]
	}
	argsObj, err := parseCallArguments(src)
	if err != nil {
		reason := reasonInvalidJSON
		if errors.Is(err, errArgsTooLarge) {
			reason = reasonInvalidSize
		} else if strings.Contains(err.Error(), "unreadable") || strings.Contains(err.Error(), "file path") {
			reason = reasonInvalidFile
		}
		return invalidCallErr(asJSON, "", name, "input", reason, err.Error())
	}
	client, err := normalizeCLIClient(callAs, cmd.Flags().Changed("as"))
	if err != nil {
		return invalidCallErr(asJSON, "", name, "input", reasonInvalidIdentity, "invalid --as value")
	}

	port, err := resolveRunningPort("call", callStack)
	if err != nil {
		return daemonUnavailableError(asJSON, err)
	}
	api := newDaemonAPI(port, 0)
	payload, _ := json.Marshal(struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
		Client    string         `json:"client"`
	}{Name: name, Arguments: argsObj, Client: client})

	ctx, cancel := contextTimeout(cmd.Context(), callTimeout)
	defer cancel()
	resp, err := api.originDo(ctx, http.MethodPost, "/api/tools/call", bytes.NewReader(payload), "application/json")
	if err != nil {
		return mapCallTransport(asJSON, client, name, err, ctx)
	}
	body, err := readCappedResponse(resp)
	if err != nil {
		return invalidCallErr(asJSON, client, name, "daemon", reasonResponseTooLarge, "gateway response exceeds 16 MiB")
	}
	if resp.StatusCode != http.StatusOK {
		return classifyHTTPFailure(resp, body, asJSON, client, name)
	}
	env, err := decodeCallEnvelope(body)
	if err != nil {
		return invalidCallErr(asJSON, client, name, "daemon", reasonMalformedResponse, "malformed gateway response")
	}
	if env.Client == "" {
		env.Client = client
	}
	if env.Name == "" {
		env.Name = name
	}
	exit2 := env.Outcome.Disposition == runs.DispositionToolError && env.Outcome.Stage == runs.StageDownstream && env.Outcome.Reason == runs.ReasonToolError
	if env.Outcome.Completion == mcp.CompletionInputRequired {
		exit2 = false
	}
	if asJSON {
		if env.Outcome.Reason == runs.ReasonOK && env.Error == nil {
			return output.EncodeJSON(cmd.OutOrStdout(), env)
		}
		human := "tool call failed"
		if env.Error != nil && env.Error.Message != "" {
			human = env.Error.Message
		}
		return &commandError{envelope: env, human: human, asJSON: true, exit2: exit2}
	}
	if env.Outcome.Reason == runs.ReasonOK && env.Error == nil {
		renderHumanCallResult(cmd, env)
		return nil
	}
	human := "tool call failed"
	if env.Error != nil && env.Error.Message != "" {
		human = env.Error.Message
	}
	return &commandError{envelope: env, human: human, asJSON: false, exit2: exit2}
}

func callJSONMode(cmd *cobra.Command) (bool, error) {
	format, err := resolveFormat(callFormat, cmd.Flags().Changed("format"), callJSON != nil && *callJSON)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(format, "json"), nil
}

func invalidCallErr(asJSON bool, client, name, stage, reason, message string) *commandError {
	return newCommandError(asJSON, localFailureEnvelope(client, name, stage, reason, message), message, false)
}

func mapCallTransport(asJSON bool, client, name string, err error, ctx context.Context) *commandError {
	if errors.Is(err, errRedirectRefused) {
		return invalidCallErr(asJSON, client, name, "daemon", reasonRedirectRefused, "refused to follow a redirect")
	}
	if ctx.Err() == context.DeadlineExceeded {
		return invalidCallErr(asJSON, client, name, "daemon", "deadline_exceeded", "tool call timed out")
	}
	if ctx.Err() == context.Canceled {
		return invalidCallErr(asJSON, client, name, "daemon", "context_canceled", "tool call was canceled")
	}
	return invalidCallErr(asJSON, client, name, "daemon", reasonUnexpectedStatus, "gateway request failed")
}

func renderHumanCallResult(cmd *cobra.Command, env cliCallEnvelope) {
	out := cmd.OutOrStdout()
	if env.Result == nil {
		fmt.Fprintln(out, "ok")
		return
	}
	printed := false
	for _, block := range env.Result.Content {
		switch block.Type {
		case "text":
			fmt.Fprintln(out, sanitizeTerminal(block.Text))
			printed = true
		default:
			fmt.Fprintf(out, "[%s content]\n", sanitizeTerminal(block.Type))
			printed = true
		}
	}
	if !printed {
		fmt.Fprintln(out, "ok")
	}
}
