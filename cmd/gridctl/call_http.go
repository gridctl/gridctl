package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runs"
)

const callResponseMaxBytes = 16 << 20

var (
	errRedirectRefused     = errors.New("redirect refused")
	errInvalidRequestPath  = errors.New("invalid request path")
	errResponseTooLarge    = errors.New("response exceeds 16 MiB")
	errMalformedResponse   = errors.New("malformed gateway response")
	errIncompatibleSchema  = errors.New("incompatible gateway response")
)

func (a *daemonAPI) originDo(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	if path == "" || !strings.HasPrefix(path, "/") || strings.Contains(path, "://") {
		return nil, errInvalidRequestPath
	}
	req, err := http.NewRequestWithContext(ctx, method, a.URL(path), body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	a.authorize(req)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errRedirectRefused
		},
	}
	return client.Do(req)
}

func readCappedResponse(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, callResponseMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > callResponseMaxBytes {
		return nil, errResponseTooLarge
	}
	return data, nil
}

func mapResponseRead(asJSON bool, client, name string, err error) *commandError {
	if errors.Is(err, errResponseTooLarge) {
		return uncertainFailure(asJSON, client, name, runs.DispositionTransportError, reasonResponseTooLarge, "gateway response exceeds 16 MiB")
	}
	return uncertainFailure(asJSON, client, name, runs.DispositionTransportError, reasonTransportError, "gateway response could not be read")
}

func isJSONContentType(ct string) bool {
	media, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return strings.EqualFold(media, "application/json")
}

func classifyHTTPFailure(resp *http.Response, body []byte, asJSON bool, client, name string) *commandError {
	if resp.Header.Get("Gridctl-Auth-Rejected") == "1" {
		env := localFailureEnvelope(client, name, "auth", reasonGatewayAuthRejected, "gateway authentication rejected")
		return newCommandError(asJSON, env, "gateway authentication rejected", false)
	}
	if resp.StatusCode == http.StatusForbidden {
		env := localFailureEnvelope(client, name, "auth", reasonHostRejected, "request rejected by host policy")
		return newCommandError(asJSON, env, "request rejected by host policy", false)
	}
	if isJSONContentType(resp.Header.Get("Content-Type")) {
		if env, err := decodeCallEnvelope(body); err == nil && env.Error != nil {
			human := env.Error.Message
			if human == "" {
				human = env.Outcome.Reason
			}
			exit2 := callEnvelopeCompletedToolError(env)
			if env.Outcome.Completion == mcp.CompletionInputRequired {
				exit2 = false
			}
			return &commandError{envelope: env, human: human, asJSON: asJSON, exit2: exit2}
		}
	}
	if resp.StatusCode == http.StatusNotFound {
		env := localFailureEnvelope(client, name, "daemon", reasonMissingEndpoint, "running daemon does not support this command")
		return newCommandError(asJSON, env, "running daemon does not support this command; upgrade the gateway", false)
	}
	env := localFailureEnvelope(client, name, "daemon", reasonUnexpectedStatus, "unexpected gateway response")
	return newCommandError(asJSON, env, "unexpected gateway response", false)
}

func daemonUnavailableError(asJSON bool, err error) *commandError {
	msg := "gateway not running; try `gridctl status`"
	if err != nil && strings.Contains(err.Error(), "multiple stacks") {
		msg = err.Error()
	} else if err != nil && strings.Contains(err.Error(), "is not running") {
		msg = err.Error()
	} else if err != nil && strings.Contains(err.Error(), "could not read state") {
		msg = "could not read daemon state"
	}
	reason := reasonDaemonUnavailable
	env := localFailureEnvelope("", "", "daemon", reason, msg)
	return newCommandError(asJSON, env, msg, false)
}

func contextTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, d)
}

func decodeCallEnvelope(body []byte) (cliCallEnvelope, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var env cliCallEnvelope
	if err := dec.Decode(&env); err != nil {
		return cliCallEnvelope{}, err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return cliCallEnvelope{}, errMalformedResponse
	}
	if env.SchemaVersion != callSchemaVersion {
		return cliCallEnvelope{}, errIncompatibleSchema
	}
	if env.Outcome.Disposition == "" || env.Outcome.Stage == "" || env.Outcome.Reason == "" || env.Outcome.Completion == "" {
		return cliCallEnvelope{}, errMalformedResponse
	}
	if !validCompletion(env.Outcome.Completion) {
		return cliCallEnvelope{}, errMalformedResponse
	}
	if env.Outcome.Reason == runs.ReasonOK && !callEnvelopeSuccess(env) {
		return cliCallEnvelope{}, errMalformedResponse
	}
	return env, nil
}

func validCompletion(c string) bool {
	switch c {
	case mcp.CompletionComplete, mcp.CompletionNotStarted, mcp.CompletionInputRequired, mcp.CompletionUnknown:
		return true
	default:
		return false
	}
}

func decodeDiscoverEnvelope(body []byte) (mcp.ToolDiscoverResult, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return mcp.ToolDiscoverResult{}, err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return mcp.ToolDiscoverResult{}, errMalformedResponse
	}
	if raw == nil {
		return mcp.ToolDiscoverResult{}, errMalformedResponse
	}
	for _, key := range []string{"schema_version", "client", "tools", "total_visible", "matched", "returned", "truncated"} {
		if _, ok := raw[key]; !ok {
			return mcp.ToolDiscoverResult{}, errMalformedResponse
		}
	}
	if bytes.Equal(bytes.TrimSpace(raw["tools"]), []byte("null")) {
		return mcp.ToolDiscoverResult{}, errMalformedResponse
	}
	var env mcp.ToolDiscoverResult
	if err := json.Unmarshal(body, &env); err != nil {
		return mcp.ToolDiscoverResult{}, err
	}
	if env.SchemaVersion != callSchemaVersion || env.Tools == nil {
		return mcp.ToolDiscoverResult{}, errIncompatibleSchema
	}
	return env, nil
}
