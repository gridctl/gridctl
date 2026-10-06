package mcp

import (
	"encoding/json"
	"log/slog"

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
	method   string
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

	var method string
	_ = json.Unmarshal(methodJSON, &method)

	idJSON, hasID := envelope["id"]
	if !hasID {
		return stdioMessage{kind: stdioNotification, method: method}, nil
	}

	id := json.RawMessage(append([]byte(nil), idJSON...))
	if method == "ping" {
		reply := jsonrpc.NewSuccessResponse(&id, struct{}{})
		return stdioMessage{kind: stdioRequest, method: method, reply: &reply}, nil
	}

	reply := jsonrpc.NewErrorResponse(&id, jsonrpc.MethodNotFound, "Method not found")
	return stdioMessage{kind: stdioRequest, method: method, reply: &reply}, nil
}

// logStdioPeer names a dropped notification or a rejected server request.
// Ping replies are not logged. Params are never included.
func logStdioPeer(logger *slog.Logger, message stdioMessage) {
	if logger == nil {
		return
	}
	switch message.kind {
	case stdioNotification:
		logger.Debug("server notification dropped", "method", message.method)
	case stdioRequest:
		if message.reply != nil && message.reply.Error != nil {
			logger.Debug("server request rejected", "method", message.method, "code", message.reply.Error.Code)
		}
	}
}
