package anthropic

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/gridctl/gridctl/pkg/agent"
)

// streamEvent is one SSE frame parsed off the wire. Anthropic emits
// frames of the form `event: <type>\ndata: <json>\n\n`.
type streamEvent struct {
	Event string
	Data  []byte
}

// readStream drains the SSE stream into a slice of agent.ChatChunk.
// The function deliberately accumulates rather than emitting via a
// goroutine: Phase B's StreamReader is backed by a slice, and chunks
// are small (a few hundred bytes apiece). A future iteration may
// switch to a goroutine + channel reader without breaking the public
// signature, since callers see only StreamReader.
func readStream(body io.Reader, logger *slog.Logger) ([]agent.ChatChunk, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var (
		chunks         []agent.ChatChunk
		event          string
		dataLines      []string
		toolCallIndex  = -1 // current content block index that is a tool_use
		toolCallActive bool
	)

	flush := func() error {
		if event == "" && len(dataLines) == 0 {
			return nil
		}
		raw := []byte(strings.Join(dataLines, "\n"))
		dataLines = dataLines[:0]
		ev := streamEvent{Event: event, Data: raw}
		event = ""

		chunk, ok, err := decodeEvent(ev, &toolCallIndex, &toolCallActive, logger)
		if err != nil {
			return err
		}
		if ok {
			chunks = append(chunks, chunk)
		}
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
			// Other line shapes — SSE keepalive comments (": ...") and
			// frames the package does not use — fall through silently.
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("anthropic: read stream: %w", err)
	}
	// Flush any trailing event without a blank-line terminator.
	if err := flush(); err != nil {
		return nil, err
	}
	return chunks, nil
}

// decodeEvent translates a single SSE frame into a ChatChunk. The
// returned bool reports whether the frame produced a chunk (some
// frame types — message_start, content_block_stop, ping — carry only
// metadata or zero-content deltas that the runtime ignores).
func decodeEvent(
	ev streamEvent,
	toolCallIndex *int,
	toolCallActive *bool,
	logger *slog.Logger,
) (agent.ChatChunk, bool, error) {
	switch ev.Event {
	case "message_start":
		return agent.ChatChunk{}, false, nil

	case "content_block_start":
		var p struct {
			Index        int `json:"index"`
			ContentBlock struct {
				Type string          `json:"type"`
				ID   string          `json:"id"`
				Name string          `json:"name"`
				Text string          `json:"text"`
				Input json.RawMessage `json:"input"`
			} `json:"content_block"`
		}
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return agent.ChatChunk{}, false, fmt.Errorf("anthropic: decode content_block_start: %w", err)
		}
		if p.ContentBlock.Type == "tool_use" {
			*toolCallIndex = p.Index
			*toolCallActive = true
			return agent.ChatChunk{
				ToolCallDelta: &agent.ToolCallDelta{
					Index: p.Index,
					ID:    p.ContentBlock.ID,
					Name:  p.ContentBlock.Name,
				},
			}, true, nil
		}
		// Text blocks rarely carry initial text in content_block_start;
		// when they do (rare, but possible), surface it as a delta.
		if p.ContentBlock.Type == "text" && p.ContentBlock.Text != "" {
			return agent.ChatChunk{Delta: p.ContentBlock.Text}, true, nil
		}
		*toolCallActive = false
		return agent.ChatChunk{}, false, nil

	case "content_block_delta":
		var p struct {
			Index int `json:"index"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
		}
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return agent.ChatChunk{}, false, fmt.Errorf("anthropic: decode content_block_delta: %w", err)
		}
		switch p.Delta.Type {
		case "text_delta":
			if p.Delta.Text == "" {
				return agent.ChatChunk{}, false, nil
			}
			return agent.ChatChunk{Delta: p.Delta.Text}, true, nil
		case "input_json_delta":
			if p.Delta.PartialJSON == "" {
				return agent.ChatChunk{}, false, nil
			}
			return agent.ChatChunk{
				ToolCallDelta: &agent.ToolCallDelta{
					Index:     p.Index,
					ArgsDelta: p.Delta.PartialJSON,
				},
			}, true, nil
		}
		return agent.ChatChunk{}, false, nil

	case "content_block_stop":
		*toolCallActive = false
		return agent.ChatChunk{}, false, nil

	case "message_delta":
		var p struct {
			Delta struct {
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Usage wireUsage `json:"usage"`
		}
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return agent.ChatChunk{}, false, fmt.Errorf("anthropic: decode message_delta: %w", err)
		}
		chunk := agent.ChatChunk{}
		if p.Usage.OutputTokens > 0 || p.Usage.InputTokens > 0 || p.Usage.CacheReadInputTokens > 0 || p.Usage.CacheCreationInputTokens > 0 {
			chunk.Usage = &agent.Usage{
				InputTokens:      p.Usage.InputTokens,
				OutputTokens:     p.Usage.OutputTokens,
				CacheReadTokens:  p.Usage.CacheReadInputTokens,
				CacheWriteTokens: p.Usage.CacheCreationInputTokens,
			}
		}
		if p.Delta.StopReason != "" {
			chunk.StopReason = translateStopReason(p.Delta.StopReason)
		}
		if chunk.Delta == "" && chunk.ToolCallDelta == nil && chunk.Usage == nil && chunk.StopReason == "" {
			return agent.ChatChunk{}, false, nil
		}
		return chunk, true, nil

	case "message_stop", "ping":
		return agent.ChatChunk{}, false, nil

	case "error":
		// Anthropic emits SSE errors mid-stream (e.g. overloaded).
		var p struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(ev.Data, &p); err == nil && p.Error.Message != "" {
			return agent.ChatChunk{}, false, fmt.Errorf("anthropic stream: %s (%s)", p.Error.Message, p.Error.Type)
		}
		return agent.ChatChunk{}, false, fmt.Errorf("anthropic stream: error event")

	default:
		if logger != nil {
			logger.Debug("anthropic.stream: unknown event", slog.String("event", ev.Event))
		}
		return agent.ChatChunk{}, false, nil
	}
}
