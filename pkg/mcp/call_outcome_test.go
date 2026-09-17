package mcp

import (
	"context"
	"errors"
	"sync"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/gridctl/gridctl/pkg/runs"
)

func TestClassifyCall_Table(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	deadline, dcancel := context.WithTimeout(context.Background(), 0)
	defer dcancel()
	<-deadline.Done()

	tests := []struct {
		name     string
		result   *ToolCallResult
		err      error
		ctx      context.Context
		wantDisp string
		wantStg  string
		wantRsn  string
		wantCmp  string
		wantDet  string
	}{
		{
			name:     "success",
			result:   &ToolCallResult{Content: []Content{NewTextContent("ok")}},
			ctx:      context.Background(),
			wantDisp: runs.DispositionCompleted,
			wantStg:  runs.StageDownstream,
			wantRsn:  runs.ReasonOK,
			wantCmp:  CompletionComplete,
		},
		{
			name:     "tool error",
			result:   &ToolCallResult{IsError: true, Content: []Content{NewTextContent("no")}},
			ctx:      context.Background(),
			wantDisp: runs.DispositionToolError,
			wantStg:  runs.StageDownstream,
			wantRsn:  runs.ReasonToolError,
			wantCmp:  CompletionComplete,
		},
		{
			name:     "input required precedes isError",
			result:   &ToolCallResult{IsError: true, ResultType: ResultTypeInputRequired},
			ctx:      context.Background(),
			wantDisp: runs.DispositionInputRequired,
			wantStg:  runs.StageDownstream,
			wantRsn:  runs.ReasonInputRequired,
			wantCmp:  CompletionInputRequired,
		},
		{
			name:     "request state is input required",
			result:   &ToolCallResult{RequestState: "opaque"},
			ctx:      context.Background(),
			wantDisp: runs.DispositionInputRequired,
			wantStg:  runs.StageDownstream,
			wantRsn:  runs.ReasonInputRequired,
			wantCmp:  CompletionInputRequired,
		},
		{
			name:     "transport error",
			err:      errors.New("connection lost"),
			ctx:      context.Background(),
			wantDisp: runs.DispositionTransportError,
			wantStg:  runs.StageDownstream,
			wantRsn:  runs.ReasonTransportError,
			wantCmp:  CompletionUnknown,
		},
		{
			name:     "canceled",
			err:      context.Canceled,
			ctx:      canceled,
			wantDisp: runs.DispositionCancelled,
			wantStg:  runs.StageDownstream,
			wantRsn:  runs.ReasonContextCanceled,
			wantCmp:  CompletionUnknown,
		},
		{
			name:     "deadline",
			err:      context.DeadlineExceeded,
			ctx:      deadline,
			wantDisp: runs.DispositionTimeout,
			wantStg:  runs.StageDownstream,
			wantRsn:  runs.ReasonDeadlineExceeded,
			wantCmp:  CompletionUnknown,
		},
		{
			name:     "execution admission",
			err:      &ExecutionAdmissionError{err: errors.New("execution: required enforcement evidence unavailable")},
			ctx:      context.Background(),
			wantDisp: runs.DispositionTransportError,
			wantStg:  runs.StageDownstream,
			wantRsn:  runs.ReasonTransportError,
			wantCmp:  CompletionNotStarted,
			wantDet:  DetailExecutionAdmission,
		},
		{
			name:     "execution admission canceled",
			err:      &ExecutionAdmissionError{err: context.Canceled},
			ctx:      canceled,
			wantDisp: runs.DispositionCancelled,
			wantStg:  runs.StageDownstream,
			wantRsn:  runs.ReasonContextCanceled,
			wantCmp:  CompletionUnknown,
			wantDet:  DetailExecutionAdmission,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyCall(tt.result, tt.err, tt.ctx)
			if got.Disposition != tt.wantDisp || got.Stage != tt.wantStg || got.Reason != tt.wantRsn || got.Completion != tt.wantCmp || got.Detail != tt.wantDet {
				t.Fatalf("classifyCall = %+v", got)
			}
		})
	}
}

func TestClassifyCall_RecorderDisabled(t *testing.T) {
	g := NewGateway()
	ctrl := gomock.NewController(t)
	client := setupMockAgentClient(ctrl, "agent1", []Tool{{Name: "echo"}})
	client.EXPECT().CallTool(gomock.Any(), "echo", gomock.Any()).Return(&ToolCallResult{
		Content: []Content{NewTextContent("ok")},
	}, nil)
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	result, out, err := g.CallCanonicalTool(context.Background(), ToolCallParams{Name: "agent1__echo"})
	if err != nil || result.IsError {
		t.Fatalf("call failed: %v %+v", err, result)
	}
	if out.Disposition != runs.DispositionCompleted || out.Reason != runs.ReasonOK || out.Completion != CompletionComplete {
		t.Fatalf("outcome = %+v", out)
	}
}

func TestCallCanonicalTool_ConcurrentOutcomes(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	client := setupMockAgentClient(ctrl, "agent1", []Tool{{Name: "echo"}, {Name: "fail"}})
	client.EXPECT().CallTool(gomock.Any(), "echo", gomock.Any()).DoAndReturn(
		func(context.Context, string, map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{Content: []Content{NewTextContent("ok")}}, nil
		},
	).AnyTimes()
	client.EXPECT().CallTool(gomock.Any(), "fail", gomock.Any()).DoAndReturn(
		func(context.Context, string, map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{IsError: true, Content: []Content{NewTextContent("no")}}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make(chan string, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			name := "agent1__echo"
			want := runs.DispositionCompleted
			if i%2 == 1 {
				name = "agent1__fail"
				want = runs.DispositionToolError
			}
			_, out, err := g.CallCanonicalTool(context.Background(), ToolCallParams{Name: name})
			if err != nil {
				errs <- err.Error()
				return
			}
			if out.Disposition != want {
				errs <- out.Disposition
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for msg := range errs {
		t.Fatalf("concurrent outcome mixed: %s", msg)
	}
}

func TestExecutionAdmissionError_UnwrapsCancel(t *testing.T) {
	err := &ExecutionAdmissionError{err: context.Canceled}
	if !errors.Is(err, context.Canceled) {
		t.Fatal("wrapped admission error must preserve errors.Is cancellation")
	}
}
