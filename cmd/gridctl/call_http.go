package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

const callResponseMaxBytes = 16 << 20

var errRedirectRefused = errors.New("redirect refused")

func (a *daemonAPI) originDo(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	if path == "" || !strings.HasPrefix(path, "/") || strings.Contains(path, "://") {
		return nil, fmt.Errorf("invalid request path")
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
		return nil, fmt.Errorf("response exceeds 16 MiB")
	}
	return data, nil
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
	if resp.StatusCode == http.StatusNotFound {
		env := localFailureEnvelope(client, name, "daemon", reasonMissingEndpoint, "running daemon does not support this command")
		return newCommandError(asJSON, env, "running daemon does not support this command; upgrade the gateway", false)
	}
	var env cliCallEnvelope
	if json.Unmarshal(body, &env) == nil && env.SchemaVersion == 1 && env.Error != nil {
		human := env.Error.Message
		if human == "" {
			human = env.Outcome.Reason
		}
		exit2 := env.Outcome.Disposition == "tool_error" && env.Outcome.Stage == "downstream" && env.Outcome.Reason == "tool_error"
		return &commandError{envelope: env, human: human, asJSON: asJSON, exit2: exit2}
	}
	env = localFailureEnvelope(client, name, "daemon", reasonUnexpectedStatus, "unexpected gateway response")
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
	var env cliCallEnvelope
	dec := json.NewDecoder(strings.NewReader(string(body)))
	if err := dec.Decode(&env); err != nil {
		return cliCallEnvelope{}, err
	}
	return env, nil
}

func decodeDiscoverEnvelope(body []byte) (mcp.ToolDiscoverResult, error) {
	var env mcp.ToolDiscoverResult
	if err := json.Unmarshal(body, &env); err != nil {
		return mcp.ToolDiscoverResult{}, err
	}
	if env.Tools == nil {
		env.Tools = []mcp.Tool{}
	}
	return env, nil
}
