package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/output"
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
	if isCallOrToolsCommand(cmd) && commandWantsJSON(cmd) {
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
	if f := cmd.Flags().Lookup("format"); f != nil {
		v, _ := cmd.Flags().GetString("format")
		return strings.EqualFold(v, "json")
	}
	return false
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
	return cliCallEnvelope{
		SchemaVersion: callSchemaVersion,
		Client:        client,
		Name:          name,
		Outcome: mcp.CallOutcome{
			Disposition: "invalid_request",
			Stage:       stage,
			Reason:      reason,
			Completion:  mcp.CompletionNotStarted,
		},
		Result: nil,
		Error:  &cliErrorBody{Code: reason, Message: message},
	}
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
