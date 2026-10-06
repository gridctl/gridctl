package mcp

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

func statelessRelayRaw(outcome relayResult) json.RawMessage {
	if outcome.passThrough {
		return outcome.raw
	}
	return mergeStatelessFields(outcome.raw, outcome.fields)
}

func writeStatelessRelayError(w http.ResponseWriter, req *jsonrpc.Request, err error) {
	if resp, ok := downstreamRelayResponse(req.ID, err); ok {
		writeStatelessResponse(w, http.StatusOK, resp)
		return
	}
	writeStatelessResponse(w, http.StatusOK, jsonrpc.NewErrorResponse(req.ID, jsonrpc.InternalError, err.Error()))
}

func writeStatelessResourceError(w http.ResponseWriter, req *jsonrpc.Request, uri string, err error) {
	var nf *ResourceNotFoundError
	if errors.As(err, &nf) {
		writeStatelessResponse(w, http.StatusOK, jsonrpc.NewErrorResponseWithData(
			req.ID, jsonrpc.InvalidParams, nf.Error(), map[string]string{"uri": uri}))
		return
	}
	if resp, ok := downstreamRelayResponse(req.ID, err); ok {
		writeStatelessResponse(w, http.StatusOK, resp)
		return
	}
	writeStatelessResponse(w, http.StatusOK, jsonrpc.NewErrorResponseWithData(
		req.ID, jsonrpc.InvalidParams, err.Error(), map[string]string{"uri": uri}))
}

func handshakeRelayError(id *json.RawMessage, err error) jsonrpc.Response {
	if resp, ok := downstreamRelayResponse(id, err); ok {
		return resp
	}
	return jsonrpc.NewErrorResponse(id, jsonrpc.InternalError, err.Error())
}

func handshakeResourceError(id *json.RawMessage, uri string, err error) jsonrpc.Response {
	var nf *ResourceNotFoundError
	if errors.As(err, &nf) {
		return jsonrpc.NewErrorResponseWithData(id, ErrCodeResourceNotFound, nf.Error(), map[string]string{"uri": uri})
	}
	if resp, ok := downstreamRelayResponse(id, err); ok {
		return resp
	}
	return jsonrpc.NewErrorResponseWithData(id, ErrCodeResourceNotFound, err.Error(), map[string]string{"uri": uri})
}

func downstreamRelayResponse(id *json.RawMessage, err error) (jsonrpc.Response, bool) {
	var de *DownstreamRelayError
	if !errors.As(err, &de) {
		return jsonrpc.Response{}, false
	}
	if de.RPC != nil {
		return jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      id,
			Error:   &jsonrpc.Error{Code: de.RPC.Code, Message: de.RPC.Message, Data: de.RPC.Data},
		}, true
	}
	return jsonrpc.NewErrorResponse(id, jsonrpc.InternalError, de.Error()), true
}
