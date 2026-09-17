package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/runs"
	"github.com/spf13/cobra"
)

const (
	exitOK        = 0
	exitError     = 1
	exitToolError = 2

	callSchemaVersion = 1

	reasonInvalidJSON         = "invalid_json"
	reasonInvalidTarget       = "invalid_target"
	reasonInvalidFile         = "invalid_file"
	reasonInvalidSize         = "invalid_size"
	reasonInvalidIdentity     = "invalid_identity"
	reasonInvalidLimit        = "invalid_limit"
	reasonInvalidTimeout      = "invalid_timeout"
	reasonInvalidFlag         = "invalid_flag"
	reasonDaemonUnavailable   = "daemon_unavailable"
	reasonGatewayAuthRejected = "gateway_auth_rejected"
	reasonHostRejected        = "host_rejected"
	reasonRedirectRefused     = "redirect_refused"
	reasonResponseTooLarge    = "response_too_large"
	reasonMalformedResponse   = "malformed_response"
	reasonMissingEndpoint     = "missing_endpoint"
	reasonUnexpectedStatus    = "unexpected_status"
	reasonDeadlineExceeded    = "deadline_exceeded"
	reasonContextCanceled     = "context_canceled"
	reasonTransportError      = "transport_error"
)

type commandError struct {
	envelope any
	human    string
	asJSON   bool
	exit2    bool
}

func (e *commandError) Error() string {
	if e == nil {
		return ""
	}
	return e.human
}

func exitStatus(err error) int {
	var ce *commandError
	if errors.As(err, &ce) && ce.exit2 {
		return exitToolError
	}
	if err != nil {
		return exitError
	}
	return exitOK
}

func renderCommandError(stdout, stderr io.Writer, cmd *cobra.Command, err error) bool {
	var ce *commandError
	if errors.As(err, &ce) {
		if ce.asJSON {
			_ = output.EncodeJSON(stdout, ce.envelope)
			return true
		}
		fmt.Fprintln(stderr, sanitizeTerminal(ce.human))
		return true
	}
	if isCallOrToolsCommand(cmd) && commandRequestsJSON(cmd) {
		_ = output.EncodeJSON(stdout, localFailureEnvelope("", "", "input", reasonInvalidFlag, "invalid request"))
		return true
	}
	return false
}

func isCallOrToolsCommand(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "call", "tools":
			return true
		}
	}
	return false
}

func commandWantsJSON(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	if f := cmd.Flags().Lookup("json"); f != nil && f.Changed {
		return true
	}
	if f := cmd.Flags().Lookup("format"); f != nil && f.Changed {
		v, _ := cmd.Flags().GetString("format")
		return strings.EqualFold(v, "json")
	}
	return false
}

func commandRequestsJSON(cmd *cobra.Command) bool {
	return commandWantsJSON(cmd) || argsWantJSON(osArgsForJSON())
}

func osArgsForJSON() []string {
	if len(os.Args) > 1 {
		return os.Args[1:]
	}
	return nil
}

func argsWantJSON(args []string) bool {
	jsonSet := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		if a == "--json" {
			jsonSet = true
			continue
		}
		if strings.HasPrefix(a, "--format=") {
			if strings.EqualFold(strings.TrimPrefix(a, "--format="), "json") {
				return true
			}
			continue
		}
		if a == "--format" && i+1 < len(args) {
			if strings.EqualFold(args[i+1], "json") {
				return true
			}
			i++
		}
	}
	return jsonSet
}

type cliCallEnvelope struct {
	SchemaVersion int                 `json:"schema_version"`
	Client        string              `json:"client,omitempty"`
	Name          string              `json:"name,omitempty"`
	Outcome       mcp.CallOutcome     `json:"outcome"`
	Result        *mcp.ToolCallResult `json:"result"`
	Error         *cliErrorBody       `json:"error"`
}

type cliErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func localFailureEnvelope(client, name, stage, reason, message string) cliCallEnvelope {
	return failureEnvelope(client, name, "invalid_request", stage, reason, mcp.CompletionNotStarted, message)
}

func failureEnvelope(client, name, disposition, stage, reason, completion, message string) cliCallEnvelope {
	return cliCallEnvelope{
		SchemaVersion: callSchemaVersion,
		Client:        client,
		Name:          name,
		Outcome: mcp.CallOutcome{
			Disposition: disposition,
			Stage:       stage,
			Reason:      reason,
			Completion:  completion,
		},
		Result: nil,
		Error:  &cliErrorBody{Code: reason, Message: message},
	}
}

func uncertainFailure(asJSON bool, client, name, disposition, reason, message string) *commandError {
	return newCommandError(asJSON, failureEnvelope(client, name, disposition, "daemon", reason, mcp.CompletionUnknown, message), message, false)
}

func callEnvelopeSuccess(env cliCallEnvelope) bool {
	return env.Outcome.Disposition == runs.DispositionCompleted &&
		env.Outcome.Stage == runs.StageDownstream &&
		env.Outcome.Reason == runs.ReasonOK &&
		env.Outcome.Completion == mcp.CompletionComplete &&
		env.Error == nil
}

func callEnvelopeCompletedToolError(env cliCallEnvelope) bool {
	return env.Outcome.Disposition == runs.DispositionToolError &&
		env.Outcome.Stage == runs.StageDownstream &&
		env.Outcome.Reason == runs.ReasonToolError &&
		env.Outcome.Completion == mcp.CompletionComplete &&
		env.Result != nil && env.Result.IsError
}

func newCommandError(asJSON bool, env cliCallEnvelope, human string, exit2 bool) *commandError {
	return &commandError{envelope: env, human: human, asJSON: asJSON, exit2: exit2}
}

func sanitizeTerminal(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, s)
}
