package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

func (s *StreamableHTTPServer) handleSkillsRequest(ctx context.Context, req *jsonrpc.Request, handshake bool) jsonrpc.Response {
	if s.gateway.skillProvider() == nil {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.MethodNotFound, fmt.Sprintf("Unknown method: %s", req.Method))
	}
	var params struct {
		URI    string `json:"uri"`
		Cursor string `json:"cursor"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidParams, "Invalid "+req.Method+" params")
		}
	}
	var result any
	var err error
	switch req.Method {
	case "skills/list":
		var value *SkillsListResult
		value, err = s.gateway.HandleSkillsList(ctx, params.Cursor)
		if err == nil && handshake {
			value.StatelessResultFields = StatelessResultFields{}
		}
		result = value
	case "skills/get":
		var value *SkillsGetResult
		value, err = s.gateway.HandleSkillsGet(ctx, params.URI)
		if err == nil && handshake {
			value.StatelessResultFields = StatelessResultFields{}
		}
		result = value
	case "resources/directory/read":
		var value *ResourcesDirectoryResult
		value, err = s.gateway.HandleResourcesDirectoryRead(ctx, params.URI, params.Cursor)
		if err == nil && handshake {
			value.StatelessResultFields = StatelessResultFields{}
		}
		result = value
	}
	if errors.Is(err, errSkillsUnavailable) {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.MethodNotFound, fmt.Sprintf("Unknown method: %s", req.Method))
	}
	if errors.Is(err, errSkillCursor) {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidParams, err.Error())
	}
	var missing *ResourceNotFoundError
	if errors.As(err, &missing) {
		return jsonrpc.NewErrorResponseWithData(req.ID, jsonrpc.InvalidParams, missing.Error(), map[string]string{"uri": missing.URI})
	}
	if err != nil {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InternalError, err.Error())
	}
	return jsonrpc.NewSuccessResponse(req.ID, result)
}
