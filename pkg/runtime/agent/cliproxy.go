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

	args := []string{"--print", "--output-format", "stream-json"}
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
	cmd.Env = os.Environ()

	c.mu.Lock()
	c.cmd = cmd
	c.cancel = cancel
	c.mu.Unlock()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return "", nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		cancel()
		return "", nil, fmt.Errorf("starting claude CLI: %w", err)
	}

	var sb strings.Builder
	var inputTokens, outputTokens int
	parseErr := parseStreamJSON(ctx, stdout, &sb, &inputTokens, &outputTokens, events)

	_ = cmd.Wait()
	cancel()

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

// cliStreamEvent is a single line of stream-json output from the claude CLI.
// Fields are populated based on the event type.
type cliStreamEvent struct {
	Type         string           `json:"type"`
	Message      *cliMessageStart `json:"message,omitempty"`      // message_start
	Index        int              `json:"index"`                   // content_block_*
	ContentBlock *cliContentBlock `json:"content_block,omitempty"` // content_block_start
	Delta        *cliDelta        `json:"delta,omitempty"`         // content_block_delta / message_delta
	Usage        *cliUsage        `json:"usage,omitempty"`         // message_delta
}

type cliMessageStart struct {
	Usage cliUsage `json:"usage"`
}

type cliContentBlock struct {
	Type string `json:"type"` // "text" or "tool_use"
	ID   string `json:"id"`
	Name string `json:"name"`
}

type cliDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`         // text_delta
	StopReason  string `json:"stop_reason,omitempty"`  // message_delta
	PartialJSON string `json:"partial_json,omitempty"` // input_json_delta
}

type cliUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// pendingToolCall tracks an in-flight tool use block.
type pendingToolCall struct {
	id        string
	name      string
	inputBuf  strings.Builder
	startTime time.Time
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

	// Track tool use blocks by content block index
	pending := map[int]*pendingToolCall{}

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
			continue // skip non-JSON lines (stderr bleed-through, etc.)
		}

		switch ev.Type {
		case "message_start":
			if ev.Message != nil {
				*inputTokens += ev.Message.Usage.InputTokens
			}

		case "content_block_start":
			if ev.ContentBlock == nil {
				continue
			}
			if ev.ContentBlock.Type == "tool_use" {
				pending[ev.Index] = &pendingToolCall{
					id:        ev.ContentBlock.ID,
					name:      ev.ContentBlock.Name,
					startTime: time.Now(),
				}
			}

		case "content_block_delta":
			if ev.Delta == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				if ev.Delta.Text != "" {
					sb.WriteString(ev.Delta.Text)
					select {
					case events <- LLMEvent{Type: EventTypeToken, Data: TokenData{Text: ev.Delta.Text}}:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			case "input_json_delta":
				if p, ok := pending[ev.Index]; ok {
					p.inputBuf.WriteString(ev.Delta.PartialJSON)
				}
			}

		case "content_block_stop":
			if p, ok := pending[ev.Index]; ok {
				// Emit start + end together once input is fully assembled.
				// ServerName is extracted from the prefixed tool name (server__tool format).
				var input any
				if p.inputBuf.Len() > 0 {
					_ = json.Unmarshal([]byte(p.inputBuf.String()), &input)
				}
				serverName := serverNameFromPrefix(p.name)
				durationMs := time.Since(p.startTime).Milliseconds()
				select {
				case events <- LLMEvent{Type: EventTypeToolCallStart, Data: ToolCallStartData{
					ToolName:   p.name,
					ServerName: serverName,
					Input:      input,
				}}:
				case <-ctx.Done():
					return ctx.Err()
				}
				select {
				case events <- LLMEvent{Type: EventTypeToolCallEnd, Data: ToolCallEndData{
					ToolName:   p.name,
					Output:     "handled by CLI proxy",
					DurationMs: durationMs,
				}}:
				case <-ctx.Done():
					return ctx.Err()
				}
				delete(pending, ev.Index)
			}

		case "message_delta":
			if ev.Usage != nil {
				*outputTokens += ev.Usage.OutputTokens
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
				"url": sseURL,
			},
		},
	}
	data, _ := json.Marshal(cfg)
	return string(data)
}
