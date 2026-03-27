package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const cliKillGracePeriod = 5 * time.Second

// CLIProxyClient implements LLMClient by spawning the claude CLI as a subprocess.
// Path B authentication: uses the user's local claude CLI subscription.
// Tool calls in the stream-json output are observed and forwarded as events;
// actual tool execution is handled by the CLI itself via MCP config.
type CLIProxyClient struct {
	cliPath       string // absolute path to the claude binary
	mcpConfigPath string // temp file path for --mcp-config (empty = no MCP)
	mu            sync.Mutex
	cmd           *exec.Cmd
	cancel        context.CancelFunc
}

// NewCLIProxyClient creates a CLI proxy client.
// cliPath is the absolute path to the claude binary.
// mcpConfigJSON is an optional JSON string for the --mcp-config flag; if non-empty,
// a temp file is written and passed to the CLI so it can access gridctl's MCP tools.
func NewCLIProxyClient(cliPath, mcpConfigJSON string) *CLIProxyClient {
	c := &CLIProxyClient{cliPath: cliPath}
	if mcpConfigJSON != "" {
		if path, err := writeTempMCPConfig(mcpConfigJSON); err == nil {
			c.mcpConfigPath = path
		}
	}
	return c
}

// Stream spawns the claude CLI, pipes the user message, and streams events back.
// systemPrompt is passed via --system-prompt. History is used to extract the
// last user message — the CLI manages its own session context via --print mode.
// Tool call events are emitted by observing the stream-json output; the CLI
// handles actual tool execution internally via the configured MCP server.
func (c *CLIProxyClient) Stream(
	ctx context.Context,
	systemPrompt string,
	history []Message,
	tools []Tool,
	caller ToolCaller,
	events chan<- LLMEvent,
) (string, []Message, error) {
	userMsg := lastUserMsg(history)
	if userMsg == "" {
		return "", nil, fmt.Errorf("no user message in history")
	}

	args := []string{"--print", "--verbose", "--output-format", "stream-json"}
	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}
	if c.mcpConfigPath != "" {
		args = append(args, "--mcp-config", c.mcpConfigPath)
	}
	// Pass user message as positional argument
	args = append(args, "--", userMsg)

	cmdCtx, cancel := context.WithCancel(ctx)

	cmd := exec.CommandContext(cmdCtx, c.cliPath, args...)
	cmd.Env = filterEnv(os.Environ(), "CLAUDECODE")

	c.mu.Lock()
	c.cmd = cmd
	c.cancel = cancel
	c.mu.Unlock()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return "", nil, fmt.Errorf("stdout pipe: %w", err)
	}
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		return "", nil, fmt.Errorf("starting claude CLI: %w", err)
	}

	var sb strings.Builder
	var inputTokens, outputTokens int
	parseErr := parseStreamJSON(ctx, stdout, &sb, &inputTokens, &outputTokens, events)

	_ = cmd.Wait()
	cancel()

	// If nothing was produced, treat stderr output as the error message.
	if parseErr == nil && sb.Len() == 0 && stderrBuf.Len() > 0 {
		parseErr = fmt.Errorf("claude CLI: %s", strings.TrimSpace(stderrBuf.String()))
	}

	if parseErr != nil && ctx.Err() != nil {
		return sb.String(), nil, nil // context cancelled — clean exit
	}

	if parseErr != nil {
		select {
		case events <- LLMEvent{Type: EventTypeError, Data: ErrorData{Message: parseErr.Error()}}:
		default:
		}
		select {
		case events <- LLMEvent{Type: EventTypeDone}:
		default:
		}
		return sb.String(), nil, parseErr
	}

	select {
	case events <- LLMEvent{Type: EventTypeMetrics, Data: MetricsData{
		TokensIn:  inputTokens,
		TokensOut: outputTokens,
	}}:
	default:
	}
	select {
	case events <- LLMEvent{Type: EventTypeDone}:
	default:
	}

	return sb.String(), nil, nil
}

// Close terminates any running CLI subprocess and removes the temp MCP config file.
func (c *CLIProxyClient) Close() error {
	c.mu.Lock()
	cancel := c.cancel
	cmd := c.cmd
	c.cancel = nil
	c.cmd = nil
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if cmd != nil && cmd.Process != nil {
		// Graceful shutdown: SIGTERM → wait → SIGKILL → wait
		_ = cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(cliKillGracePeriod):
			_ = cmd.Process.Kill()
			<-done // drain so goroutine exits cleanly
		}
	}

	if c.mcpConfigPath != "" {
		_ = os.Remove(c.mcpConfigPath)
		c.mcpConfigPath = ""
	}

	return nil
}

// ─── stream-json parsing ──────────────────────────────────────────────────────

// The claude CLI --verbose --output-format stream-json emits turn-level events,
// not the Anthropic API's content_block_delta SSE format.
//
// Relevant event shapes:
//
//	{"type":"assistant","message":{"content":[{"type":"text","text":"..."},{"type":"tool_use","id":"...","name":"...","input":{...}}],...}}
//	{"type":"result","result":"...","usage":{"input_tokens":N,"output_tokens":N,"cache_creation_input_tokens":N,"cache_read_input_tokens":N,...}}

type cliStreamEvent struct {
	Type    string      `json:"type"`
	Message *cliMessage `json:"message,omitempty"` // "assistant" events
	Usage   *cliUsage   `json:"usage,omitempty"`   // "result" events
}

type cliMessage struct {
	Content []cliContentItem `json:"content"`
}

type cliContentItem struct {
	Type  string          `json:"type"`            // "text" or "tool_use"
	Text  string          `json:"text,omitempty"`  // text blocks
	ID    string          `json:"id,omitempty"`    // tool_use blocks
	Name  string          `json:"name,omitempty"`  // tool_use blocks
	Input json.RawMessage `json:"input,omitempty"` // tool_use blocks
}

type cliUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// parseStreamJSON reads newline-delimited stream-json events from the claude CLI stdout,
// emits token and tool-call events, and accumulates the final text response.
func parseStreamJSON(
	ctx context.Context,
	r io.Reader,
	sb *strings.Builder,
	inputTokens, outputTokens *int,
	events chan<- LLMEvent,
) error {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var ev cliStreamEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // skip non-JSON lines
		}

		switch ev.Type {
		case "assistant":
			if ev.Message == nil {
				continue
			}
			for _, block := range ev.Message.Content {
				switch block.Type {
				case "text":
					if block.Text == "" {
						continue
					}
					sb.WriteString(block.Text)
					select {
					case events <- LLMEvent{Type: EventTypeToken, Data: TokenData{Text: block.Text}}:
					case <-ctx.Done():
						return ctx.Err()
					}
				case "tool_use":
					var input any
					if len(block.Input) > 0 {
						_ = json.Unmarshal(block.Input, &input)
					}
					serverName := serverNameFromPrefix(block.Name)
					start := time.Now()
					select {
					case events <- LLMEvent{Type: EventTypeToolCallStart, Data: ToolCallStartData{
						ToolName:   block.Name,
						ServerName: serverName,
						Input:      input,
					}}:
					case <-ctx.Done():
						return ctx.Err()
					}
					select {
					case events <- LLMEvent{Type: EventTypeToolCallEnd, Data: ToolCallEndData{
						ToolName:   block.Name,
						Output:     "handled by CLI proxy",
						DurationMs: time.Since(start).Milliseconds(),
					}}:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}

		case "result":
			if ev.Usage != nil {
				*inputTokens = ev.Usage.InputTokens + ev.Usage.CacheReadInputTokens + ev.Usage.CacheCreationInputTokens
				*outputTokens = ev.Usage.OutputTokens
			}
		}
	}

	return scanner.Err()
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// serverNameFromPrefix extracts the server name from a prefixed tool name (server__tool format).
// Returns an empty string if the name has no prefix.
func serverNameFromPrefix(toolName string) string {
	if idx := strings.Index(toolName, "__"); idx > 0 {
		return toolName[:idx]
	}
	return ""
}

// lastUserMsg extracts the content of the most recent user message in history.
func lastUserMsg(history []Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "user" {
			return history[i].Content
		}
	}
	return ""
}

// filterEnv returns a copy of env with any entries whose key matches one of the
// given names removed. Used to prevent variables like CLAUDECODE from leaking
// into spawned subprocesses.
func filterEnv(env []string, keys ...string) []string {
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		exclude := false
		for _, k := range keys {
			if strings.HasPrefix(e, k+"=") {
				exclude = true
				break
			}
		}
		if !exclude {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// writeTempMCPConfig writes a JSON MCP config to a temporary file and returns its path.
func writeTempMCPConfig(jsonContent string) (string, error) {
	f, err := os.CreateTemp("", "gridctl-mcp-config-*.json")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(jsonContent); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// MCPConfigJSON builds a claude CLI --mcp-config JSON string pointing to a gridctl SSE gateway.
func MCPConfigJSON(sseURL string) string {
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"gridctl": map[string]any{
				"type": "sse",
				"url":  sseURL,
			},
		},
	}
	data, _ := json.Marshal(cfg)
	return string(data)
}
