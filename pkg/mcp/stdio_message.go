package mcp

import (
	"encoding/json"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

type stdioMessageKind uint8

const (
	stdioResponse stdioMessageKind = iota
	stdioRequest
	stdioNotification
)

type stdioMessage struct {
	kind     stdioMessageKind
	response *jsonrpc.Response
	reply    *jsonrpc.Response
}

// classifyStdioMessage distinguishes peer requests and notifications before
// decoding messages that may be correlated as responses.
func classifyStdioMessage(line []byte) (stdioMessage, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(line, &envelope); err != nil {
		return stdioMessage{}, err
	}

	methodJSON, hasMethod := envelope["method"]
	if !hasMethod {
		var response jsonrpc.Response
		if err := json.Unmarshal(line, &response); err != nil {
			return stdioMessage{}, err
		}
		return stdioMessage{kind: stdioResponse, response: &response}, nil
	}

	idJSON, hasID := envelope["id"]
	if !hasID {
		return stdioMessage{kind: stdioNotification}, nil
	}

	id := json.RawMessage(append([]byte(nil), idJSON...))
	var method string
	_ = json.Unmarshal(methodJSON, &method)
	if method == "ping" {
		reply := jsonrpc.NewSuccessResponse(&id, struct{}{})
		return stdioMessage{kind: stdioRequest, reply: &reply}, nil
	}

	reply := jsonrpc.NewErrorResponse(&id, jsonrpc.MethodNotFound, "Method not found")
	return stdioMessage{kind: stdioRequest, reply: &reply}, nil
}
