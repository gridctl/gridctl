package registry

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// mockToolCaller implements mcp.ToolCaller for testing.
type mockToolCaller struct {
	calls   []toolCall
	results map[string]*mcp.ToolCallResult
	err     error
}

type toolCall struct {
	name      string
	arguments map[string]any
}

func (m *mockToolCaller) CallTool(_ context.Context, name string, arguments map[string]any) (*mcp.ToolCallResult, error) {
	m.calls = append(m.calls, toolCall{name: name, arguments: arguments})
	if m.err != nil {
		return nil, m.err
	}
	if result, ok := m.results[name]; ok {
		return result, nil
	}
	return &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent("default result for " + name)},
	}, nil
}

// --- executeSkill tests ---

func TestExecuteSkill_SingleStep(t *testing.T) {
	mock := &mockToolCaller{
		results: map[string]*mcp.ToolCallResult{
			"git__log": {Content: []mcp.Content{mcp.NewTextContent("commit abc123")}},
		},
	}
	store := NewStore(t.TempDir())
	srv := New(store, mock)

	skill := &Skill{
		Name:  "audit",
		Steps: []Step{{Tool: "git__log"}},
		State: StateActive,
	}

	result, err := srv.executeSkill(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", extractTextContent(result))
	}
	if got := extractTextContent(result); got != "commit abc123" {
		t.Errorf("result = %q, want %q", got, "commit abc123")
	}
	if len(mock.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.calls))
	}
	if mock.calls[0].name != "git__log" {
		t.Errorf("called %q, want %q", mock.calls[0].name, "git__log")
	}
}

func TestExecuteSkill_MultipleSteps(t *testing.T) {
	mock := &mockToolCaller{
		results: map[string]*mcp.ToolCallResult{
			"fetch":     {Content: []mcp.Content{mcp.NewTextContent("data fetched")}},
			"transform": {Content: []mcp.Content{mcp.NewTextContent("data transformed")}},
			"store":     {Content: []mcp.Content{mcp.NewTextContent("data stored")}},
		},
	}
	store := NewStore(t.TempDir())
	srv := New(store, mock)

	skill := &Skill{
		Name: "pipeline",
		Steps: []Step{
			{Tool: "fetch"},
			{Tool: "transform"},
			{Tool: "store"},
		},
		State: StateActive,
	}

	result, err := srv.executeSkill(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := extractTextContent(result); got != "data stored" {
		t.Errorf("result = %q, want %q", got, "data stored")
	}
	if len(mock.calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(mock.calls))
	}
	expectedTools := []string{"fetch", "transform", "store"}
	for i, expected := range expectedTools {
		if mock.calls[i].name != expected {
			t.Errorf("call[%d] = %q, want %q", i, mock.calls[i].name, expected)
		}
	}
}

func TestExecuteSkill_InputTemplates(t *testing.T) {
	mock := &mockToolCaller{
		results: map[string]*mcp.ToolCallResult{
			"greet": {Content: []mcp.Content{mcp.NewTextContent("hello Alice")}},
		},
	}
	store := NewStore(t.TempDir())
	srv := New(store, mock)

	skill := &Skill{
		Name: "greet",
		Steps: []Step{
			{Tool: "greet", Arguments: map[string]string{"name": "{{input.user}}"}},
		},
		State: StateActive,
	}

	result, err := srv.executeSkill(context.Background(), skill, map[string]any{"user": "Alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", extractTextContent(result))
	}
	if len(mock.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.calls))
	}
	if got, ok := mock.calls[0].arguments["name"]; !ok || got != "Alice" {
		t.Errorf("argument name = %v, want %q", got, "Alice")
	}
}

func TestExecuteSkill_StepResultTemplates(t *testing.T) {
	mock := &mockToolCaller{
		results: map[string]*mcp.ToolCallResult{
			"fetch":   {Content: []mcp.Content{mcp.NewTextContent("raw data")}},
			"process": {Content: []mcp.Content{mcp.NewTextContent("processed")}},
		},
	}
	store := NewStore(t.TempDir())
	srv := New(store, mock)

	skill := &Skill{
		Name: "chain",
		Steps: []Step{
			{Tool: "fetch"},
			{Tool: "process", Arguments: map[string]string{"data": "{{step1.result}}"}},
		},
		State: StateActive,
	}

	result, err := srv.executeSkill(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", extractTextContent(result))
	}
	if len(mock.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(mock.calls))
	}
	if got, ok := mock.calls[1].arguments["data"]; !ok || got != "raw data" {
		t.Errorf("argument data = %v, want %q", got, "raw data")
	}
}

func TestExecuteSkill_MixedTemplates(t *testing.T) {
	mock := &mockToolCaller{
		results: map[string]*mcp.ToolCallResult{
			"lookup":  {Content: []mcp.Content{mcp.NewTextContent("result123")}},
			"combine": {Content: []mcp.Content{mcp.NewTextContent("done")}},
		},
	}
	store := NewStore(t.TempDir())
	srv := New(store, mock)

	skill := &Skill{
		Name: "mixed",
		Steps: []Step{
			{Tool: "lookup"},
			{Tool: "combine", Arguments: map[string]string{
				"query": "user={{input.name}} result={{step1.result}}",
			}},
		},
		State: StateActive,
	}

	result, err := srv.executeSkill(context.Background(), skill, map[string]any{"name": "bob"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", extractTextContent(result))
	}
	if got, ok := mock.calls[1].arguments["query"]; !ok || got != "user=bob result=result123" {
		t.Errorf("argument query = %v, want %q", got, "user=bob result=result123")
	}
}

func TestExecuteSkill_ToolError(t *testing.T) {
	mock := &mockToolCaller{
		results: map[string]*mcp.ToolCallResult{
			"step1": {Content: []mcp.Content{mcp.NewTextContent("ok")}},
			"step2": {Content: []mcp.Content{mcp.NewTextContent("permission denied")}, IsError: true},
		},
	}
	store := NewStore(t.TempDir())
	srv := New(store, mock)

	skill := &Skill{
		Name: "failing",
		Steps: []Step{
			{Tool: "step1"},
			{Tool: "step2"},
			{Tool: "step3"},
		},
		State: StateActive,
	}

	result, err := srv.executeSkill(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	text := extractTextContent(result)
	if text == "" {
		t.Fatal("expected non-empty error text")
	}
	// Should mention step 2 and the tool name
	if !containsAll(text, "2/3", "step2", "permission denied") {
		t.Errorf("error text missing expected info: %q", text)
	}
	// Step 3 should not be called
	if len(mock.calls) != 2 {
		t.Errorf("expected 2 calls (stop on error), got %d", len(mock.calls))
	}
}

func TestExecuteSkill_ToolCallFailure(t *testing.T) {
	mock := &mockToolCaller{
		err: fmt.Errorf("connection refused"),
	}
	store := NewStore(t.TempDir())
	srv := New(store, mock)

	skill := &Skill{
		Name:  "broken",
		Steps: []Step{{Tool: "unreachable"}},
		State: StateActive,
	}

	result, err := srv.executeSkill(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	text := extractTextContent(result)
	if !containsAll(text, "1/1", "unreachable", "connection refused") {
		t.Errorf("error text missing expected info: %q", text)
	}
}

func TestExecuteSkill_Timeout(t *testing.T) {
	mock := &mockToolCaller{
		results: map[string]*mcp.ToolCallResult{
			"slow": {Content: []mcp.Content{mcp.NewTextContent("done")}},
		},
	}
	store := NewStore(t.TempDir())
	srv := New(store, mock)

	skill := &Skill{
		Name: "timeout",
		Steps: []Step{
			{Tool: "slow"},
			{Tool: "never-called"},
		},
		State: StateActive,
	}

	// Create an already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := srv.executeSkill(ctx, skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	text := extractTextContent(result)
	if !containsAll(text, "timed out", "step 1/2") {
		t.Errorf("error text missing expected info: %q", text)
	}
	if len(mock.calls) != 0 {
		t.Errorf("expected 0 calls with cancelled context, got %d", len(mock.calls))
	}
}

func TestExecuteSkill_NoSteps(t *testing.T) {
	mock := &mockToolCaller{}
	store := NewStore(t.TempDir())
	srv := New(store, mock)

	skill := &Skill{
		Name:  "empty",
		Steps: []Step{},
		State: StateActive,
	}

	result, err := srv.executeSkill(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := extractTextContent(result); got != "skill completed with no output" {
		t.Errorf("result = %q, want %q", got, "skill completed with no output")
	}
}

// --- CallTool tests ---

func TestCallTool_NoToolCaller(t *testing.T) {
	store := NewStore(t.TempDir())
	srv := New(store, nil)

	result, err := srv.CallTool(context.Background(), "any-skill", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	text := extractTextContent(result)
	if !containsAll(text, "no tool caller configured") {
		t.Errorf("error text = %q, expected mention of tool caller", text)
	}
}

func TestCallTool_SkillNotFound(t *testing.T) {
	mock := &mockToolCaller{}
	store := NewStore(t.TempDir())
	srv := New(store, mock)
	_ = store.Load()

	result, err := srv.CallTool(context.Background(), "nonexistent", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	text := extractTextContent(result)
	if !containsAll(text, "skill not found", "nonexistent") {
		t.Errorf("error text = %q, expected mention of skill name", text)
	}
}

func TestCallTool_InactiveSkill(t *testing.T) {
	mock := &mockToolCaller{}
	dir := t.TempDir()
	store := NewStore(dir)

	writeSkillYAML(t, dir, "draft.yaml", `name: draft-skill
description: A draft skill
steps:
  - tool: noop
state: draft
`)
	_ = store.Load()

	srv := New(store, mock)

	result, err := srv.CallTool(context.Background(), "draft-skill", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	text := extractTextContent(result)
	if !containsAll(text, "not active", "draft") {
		t.Errorf("error text = %q, expected mention of inactive state", text)
	}
}

func TestCallTool_Success(t *testing.T) {
	mock := &mockToolCaller{
		results: map[string]*mcp.ToolCallResult{
			"exec": {Content: []mcp.Content{mcp.NewTextContent("executed")}},
		},
	}
	dir := t.TempDir()
	store := NewStore(dir)

	writeSkillYAML(t, dir, "active.yaml", `name: active-skill
description: Active skill
steps:
  - tool: exec
state: active
`)
	_ = store.Load()

	srv := New(store, mock)

	result, err := srv.CallTool(context.Background(), "active-skill", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", extractTextContent(result))
	}
	if got := extractTextContent(result); got != "executed" {
		t.Errorf("result = %q, want %q", got, "executed")
	}
}

func TestCallTool_CustomTimeout(t *testing.T) {
	mock := &mockToolCaller{
		results: map[string]*mcp.ToolCallResult{
			"exec": {Content: []mcp.Content{mcp.NewTextContent("done")}},
		},
	}
	dir := t.TempDir()
	store := NewStore(dir)

	writeSkillYAML(t, dir, "timed.yaml", `name: timed-skill
description: Skill with custom timeout
steps:
  - tool: exec
timeout: "5s"
state: active
`)
	_ = store.Load()

	srv := New(store, mock)

	result, err := srv.CallTool(context.Background(), "timed-skill", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", extractTextContent(result))
	}
	if got := extractTextContent(result); got != "done" {
		t.Errorf("result = %q, want %q", got, "done")
	}
}

func TestCallTool_DefaultTimeoutApplied(t *testing.T) {
	// Use a blocking mock that records the context deadline
	var ctxDeadline time.Time
	var hasDeadline bool
	blockingMock := &deadlineCaptureMock{
		onCall: func(ctx context.Context) {
			ctxDeadline, hasDeadline = ctx.Deadline()
		},
	}
	dir := t.TempDir()
	store := NewStore(dir)

	writeSkillYAML(t, dir, "default-timeout.yaml", `name: default-timeout
description: No timeout field
steps:
  - tool: exec
state: active
`)
	_ = store.Load()

	srv := New(store, blockingMock)

	_, _ = srv.CallTool(context.Background(), "default-timeout", nil)
	if !hasDeadline {
		t.Fatal("expected context to have deadline (30s default)")
	}
	// Deadline should be roughly 30s from now (allow 5s tolerance)
	remaining := time.Until(ctxDeadline)
	if remaining < 25*time.Second || remaining > 31*time.Second {
		t.Errorf("expected ~30s deadline, got %v remaining", remaining)
	}
}

// --- resolveTemplate tests ---

func TestResolveTemplate_Input(t *testing.T) {
	execCtx := &executionContext{
		input:       map[string]any{"foo": "bar"},
		stepResults: map[int]string{},
	}
	result, err := resolveTemplate("value is {{input.foo}}", execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "value is bar" {
		t.Errorf("result = %q, want %q", result, "value is bar")
	}
}

func TestResolveTemplate_StepResult(t *testing.T) {
	execCtx := &executionContext{
		input:       map[string]any{},
		stepResults: map[int]string{1: "step output"},
	}
	result, err := resolveTemplate("got: {{step1.result}}", execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "got: step output" {
		t.Errorf("result = %q, want %q", result, "got: step output")
	}
}

func TestResolveTemplate_NoMatch(t *testing.T) {
	execCtx := &executionContext{
		input:       map[string]any{},
		stepResults: map[int]string{},
	}
	result, err := resolveTemplate("literal string", execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "literal string" {
		t.Errorf("result = %q, want %q", result, "literal string")
	}
}

func TestResolveTemplate_MissingInput(t *testing.T) {
	execCtx := &executionContext{
		input:       map[string]any{},
		stepResults: map[int]string{},
	}
	_, err := resolveTemplate("{{input.missing}}", execCtx)
	if err == nil {
		t.Fatal("expected error for missing input")
	}
	if !containsAll(err.Error(), "input argument not found", "missing") {
		t.Errorf("error = %q, expected mention of missing input", err.Error())
	}
}

func TestResolveTemplate_MissingStep(t *testing.T) {
	execCtx := &executionContext{
		input:       map[string]any{},
		stepResults: map[int]string{},
	}
	_, err := resolveTemplate("{{step5.result}}", execCtx)
	if err == nil {
		t.Fatal("expected error for missing step result")
	}
	if !containsAll(err.Error(), "step 5", "not available") {
		t.Errorf("error = %q, expected mention of missing step", err.Error())
	}
}

func TestResolveTemplate_MultipleTemplates(t *testing.T) {
	execCtx := &executionContext{
		input:       map[string]any{"x": "hello", "y": "world"},
		stepResults: map[int]string{},
	}
	result, err := resolveTemplate("{{input.x}} {{input.y}}", execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello world" {
		t.Errorf("result = %q, want %q", result, "hello world")
	}
}

func TestResolveTemplate_NumericInputValue(t *testing.T) {
	execCtx := &executionContext{
		input:       map[string]any{"count": 42},
		stepResults: map[int]string{},
	}
	result, err := resolveTemplate("count={{input.count}}", execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "count=42" {
		t.Errorf("result = %q, want %q", result, "count=42")
	}
}

// --- extractTextContent tests ---

func TestExtractTextContent(t *testing.T) {
	result := &mcp.ToolCallResult{
		Content: []mcp.Content{
			mcp.NewTextContent("hello"),
			mcp.NewTextContent("world"),
		},
	}
	if got := extractTextContent(result); got != "hello" {
		t.Errorf("extractTextContent = %q, want %q", got, "hello")
	}
}

func TestExtractTextContent_Empty(t *testing.T) {
	if got := extractTextContent(nil); got != "" {
		t.Errorf("extractTextContent(nil) = %q, want empty", got)
	}

	result := &mcp.ToolCallResult{Content: []mcp.Content{}}
	if got := extractTextContent(result); got != "" {
		t.Errorf("extractTextContent(empty) = %q, want empty", got)
	}
}

func TestExtractTextContent_NonTextContent(t *testing.T) {
	result := &mcp.ToolCallResult{
		Content: []mcp.Content{
			{Type: "image", Text: ""},
		},
	}
	if got := extractTextContent(result); got != "" {
		t.Errorf("extractTextContent(image) = %q, want empty", got)
	}
}

// --- resolveStepArguments tests ---

func TestResolveStepArguments_Empty(t *testing.T) {
	execCtx := &executionContext{
		input:       map[string]any{},
		stepResults: map[int]string{},
	}
	resolved, err := resolveStepArguments(nil, execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 0 {
		t.Errorf("expected empty map, got %d entries", len(resolved))
	}
}

func TestResolveStepArguments_MixedArgs(t *testing.T) {
	execCtx := &executionContext{
		input:       map[string]any{"repo": "github.com/test"},
		stepResults: map[int]string{1: "abc123"},
	}
	args := map[string]string{
		"url":    "{{input.repo}}",
		"commit": "{{step1.result}}",
		"flag":   "literal",
	}
	resolved, err := resolveStepArguments(args, execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved["url"] != "github.com/test" {
		t.Errorf("url = %v, want %q", resolved["url"], "github.com/test")
	}
	if resolved["commit"] != "abc123" {
		t.Errorf("commit = %v, want %q", resolved["commit"], "abc123")
	}
	if resolved["flag"] != "literal" {
		t.Errorf("flag = %v, want %q", resolved["flag"], "literal")
	}
}

func TestResolveStepArguments_Error(t *testing.T) {
	execCtx := &executionContext{
		input:       map[string]any{},
		stepResults: map[int]string{},
	}
	args := map[string]string{
		"bad": "{{input.nonexistent}}",
	}
	_, err := resolveStepArguments(args, execCtx)
	if err == nil {
		t.Fatal("expected error for missing input")
	}
}

// --- helpers ---

// containsAll checks that s contains all substrings.
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// deadlineCaptureMock captures the context deadline when CallTool is invoked.
type deadlineCaptureMock struct {
	onCall func(ctx context.Context)
}

func (m *deadlineCaptureMock) CallTool(ctx context.Context, name string, arguments map[string]any) (*mcp.ToolCallResult, error) {
	if m.onCall != nil {
		m.onCall(ctx)
	}
	return &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent("ok")},
	}, nil
}
