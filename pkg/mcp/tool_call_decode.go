package mcp

import (
	"bytes"
	"encoding/json"
)

func decodeUpstreamToolCall(body []byte, params *ToolCallParams) error {
	if err := json.Unmarshal(body, params); err != nil {
		return err
	}
	var raw struct {
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return err
	}
	params.rawArguments = raw.Arguments
	return nil
}

func (params *ToolCallParams) preserveA2ANumbers() error {
	if len(params.rawArguments) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(params.rawArguments))
	decoder.UseNumber()
	if err := decoder.Decode(&params.Arguments); err != nil {
		return a2aLocalError("invalid_arguments")
	}
	return nil
}
