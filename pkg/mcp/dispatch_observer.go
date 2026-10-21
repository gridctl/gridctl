package mcp

import (
	"context"
	"time"
)

// DispatchObservation is the metadata-only record of one tools/call return.
// Sensitive executions carry the operation category in ToolName, omit
// ClientName, and include only local token estimates. The real tool name
// never enters a sensitive observation.
type DispatchObservation struct {
	ServerName         string
	ToolName           string
	ClientName         string
	ReplicaID          int
	HasReplica         bool
	Transport          string
	NetworkTransport   string
	ServerAddress      string
	ServerPort         int
	HasServerPort      bool
	MethodName         string
	OperationName      string
	ProtocolVersion    string
	TotalDuration      time.Duration
	DownstreamDuration time.Duration
	Disposition        string
	Stage              string
	Reason             string
	ErrorType          string
	Sensitive          bool
	Usage              ToolCallSummary
	HasUsage           bool
}

// DispatchObserver is notified once at the end of every tools/call return
// path. The metrics package implements it. The gateway does not import the
// metric SDK.
type DispatchObserver interface {
	ObserveDispatch(ctx context.Context, obs DispatchObservation)
}
