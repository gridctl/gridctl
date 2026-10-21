package mcp

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/runs"
)

func TestSelectErrorType(t *testing.T) {
	code := func(n int) *int { return &n }
	tests := []struct {
		name string
		in   ErrorTypeInput
		want string
	}{
		{"classifying code", ErrorTypeInput{RPCCode: code(-32603)}, "-32603"},
		{"excluded code", ErrorTypeInput{RPCCode: code(-32602), ToolIsError: true, Disposition: runs.DispositionTransportError}, ""},
		{"parse error excluded", ErrorTypeInput{RPCCode: code(-32700)}, ""},
		{"invalid request excluded", ErrorTypeInput{RPCCode: code(-32600)}, ""},
		{"method not found excluded", ErrorTypeInput{RPCCode: code(-32601)}, ""},
		{"not found excluded", ErrorTypeInput{RPCCode: code(-32002)}, ""},
		{"tool isError", ErrorTypeInput{ToolIsError: true, Disposition: runs.DispositionToolError}, runs.DispositionToolError},
		{"denied", ErrorTypeInput{Disposition: runs.DispositionDenied, Reason: runs.ReasonClientScope}, runs.DispositionDenied},
		{"routing failed", ErrorTypeInput{Disposition: runs.DispositionRoutingFailed, Reason: runs.ReasonUnknownTool}, runs.DispositionRoutingFailed},
		{"transport", ErrorTypeInput{Disposition: runs.DispositionTransportError, Reason: runs.ReasonTransportError}, runs.DispositionTransportError},
		{"cancelled spelling", ErrorTypeInput{Disposition: runs.DispositionCancelled, Reason: runs.ReasonContextCanceled}, runs.DispositionCancelled},
		{"timeout", ErrorTypeInput{Disposition: runs.DispositionTimeout, Reason: runs.ReasonDeadlineExceeded}, runs.DispositionTimeout},
		{"reason only context_canceled", ErrorTypeInput{Reason: runs.ReasonContextCanceled}, runs.ReasonContextCanceled},
		{"reason only deadline", ErrorTypeInput{Reason: runs.ReasonDeadlineExceeded}, runs.ReasonDeadlineExceeded},
		{"retry rejected", ErrorTypeInput{Disposition: runs.DispositionRetryRejected, Reason: runs.ReasonMRTRMismatch}, runs.DispositionRetryRejected},
		{"completed", ErrorTypeInput{Disposition: runs.DispositionCompleted, Reason: runs.ReasonOK}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SelectErrorType(tt.in); got != tt.want {
				t.Fatalf("SelectErrorType = %q, want %q", got, tt.want)
			}
		})
	}
	if runs.DispositionCancelled != "cancelled" {
		t.Fatalf("runs cancellation constant = %q, want cancelled", runs.DispositionCancelled)
	}
	if runs.ReasonContextCanceled == "canceled" || runs.DispositionCancelled == "canceled" {
		t.Fatal("error.type must not use the invented canceled spelling")
	}
}
