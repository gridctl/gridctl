package a2aclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestEncodeRequest(t *testing.T) {
	for _, version := range []string{"1.0", "0.3"} {
		for _, immediate := range []bool{false, true} {
			r := Request{ID: "request", MessageID: "message", Text: "hello", Data: map[string]any{"n": json.Number("9007199254740993")},
				ContextID: "context", TaskID: "task", SkillID: "skill", ReturnImmediately: immediate, OutputModes: []string{"text/plain"}}
			body, err := EncodeRequest(version, Send, r)
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				Method string `json:"method"`
				Params struct {
					Message       map[string]any `json:"message"`
					Configuration map[string]any `json:"configuration"`
				} `json:"params"`
			}
			if err := decodeJSON(body, &wire); err != nil {
				t.Fatal(err)
			}
			if wire.Method != method(version, Send) || wire.Params.Message["taskId"] != "task" || wire.Params.Message["contextId"] != "context" {
				t.Fatalf("wrong message: %s", body)
			}
			if version == "1.0" && wire.Params.Configuration["returnImmediately"] != immediate || version == "0.3" && wire.Params.Configuration["blocking"] != !immediate {
				t.Fatalf("wrong blocking option: %s", body)
			}
			if !strings.Contains(string(body), "9007199254740993") {
				t.Fatal("rounded data")
			}
		}
		for _, op := range []Operation{Get, Cancel} {
			body, err := EncodeRequest(version, op, Request{ID: "request", TaskID: "task"})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(body), method(version, op)) || op == Get && !strings.Contains(string(body), `"historyLength":0`) {
				t.Fatalf("wrong request: %s", body)
			}
		}
		body, err := EncodeRequest(version, Send, Request{ID: "request", MessageID: "message", Text: ""})
		if err != nil || strings.Contains(string(body), "contextId") || strings.Contains(string(body), "taskId") {
			t.Fatalf("fresh send: %s, %v", body, err)
		}
	}
}

func TestPart_MarshalJSON(t *testing.T) {
	for _, tc := range []struct {
		part Part
		want string
	}{
		{Part{Type: "text", Text: ""}, `{"text":"","type":"text"}`},
		{Part{Type: "data", Data: map[string]any{}}, `{"data":{},"type":"data"}`},
		{Part{Type: "data", Data: map[string]any{"n": json.Number("9007199254740993")}}, `{"data":{"n":9007199254740993},"type":"data"}`},
	} {
		body, err := json.Marshal(tc.part)
		if err != nil || string(body) != tc.want {
			t.Fatalf("lost part content: %s %v", body, err)
		}
	}
	for _, p := range []Part{{Type: "data"}, {Type: "file"}, {Type: "data", Data: map[string]any{"invalid": make(chan int)}}} {
		if _, err := json.Marshal(p); err == nil {
			t.Fatal("invalid part serialized")
		}
	}
}

func TestEncodeRequest_RejectInvalid(t *testing.T) {
	for _, tc := range []struct {
		version string
		op      Operation
		r       Request
	}{
		{"2.0", Get, Request{ID: "r", TaskID: "t"}},
		{"1.0", 99, Request{ID: "r"}},
		{"1.0", Get, Request{TaskID: "t"}},
		{"1.0", Get, Request{ID: "r"}},
		{"1.0", Get, Request{ID: "r", TaskID: "t", HistoryLength: 101}},
		{"1.0", Get, Request{ID: "r", TaskID: "t", HistoryLength: -1}},
		{"1.0", Send, Request{ID: "r"}},
		{"1.0", Send, Request{ID: "r", MessageID: "m", TaskID: "t"}},
		{"1.0", Send, Request{ID: "r", MessageID: "m", Text: strings.Repeat("x", 256<<10), Data: map[string]any{"a": 1}}},
		{"1.0", Send, Request{ID: "r", MessageID: "m", Data: map[string]any{"bad": make(chan int)}}},
	} {
		if _, err := EncodeRequest(tc.version, tc.op, tc.r); err == nil {
			t.Fatalf("accepted invalid request: %+v", tc)
		}
	}
}

func rpcBody(result string) []byte {
	return []byte(`{"jsonrpc":"2.0","id":"request","result":` + result + `}`)
}

func TestDecodeResponse_SDKReferences(t *testing.T) {
	for _, version := range []string{"1.0", "0.3"} {
		body, err := os.ReadFile("testdata/sdk-" + version + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var cases struct {
			Message json.RawMessage   `json:"message"`
			Task    json.RawMessage   `json:"task"`
			Parts   []json.RawMessage `json:"parts"`
		}
		if err := json.Unmarshal(body, &cases); err != nil {
			t.Fatal(err)
		}
		for _, op := range []Operation{Send, Get, Cancel} {
			result := string(cases.Task)
			if op == Send && version == "1.0" {
				result = `{"task":` + result + `}`
			}
			got, err := DecodeResponse(version, op, "request", 200, rpcBody(result))
			if err != nil {
				t.Fatal(version, op, err)
			}
			if got.Kind != "task" || got.TaskID != "task-1" || got.ContextID != "context-1" || got.State != "working" {
				t.Fatalf("bad task: %+v", got)
			}
		}
		result := string(cases.Message)
		if version == "1.0" {
			result = `{"message":` + result + `}`
		}
		got, err := DecodeResponse(version, Send, "request", 200, rpcBody(result))
		if err != nil || len(got.Parts) != 1 || got.Parts[0].Text != "hello" {
			t.Fatalf("message: %+v %v", got, err)
		}
		for i, raw := range cases.Parts {
			parts, err := decodeParts(version, []json.RawMessage{raw})
			if i < 2 {
				if err != nil || len(parts) != 1 {
					t.Fatalf("supported reference: %s %v", raw, err)
				}
			} else if err == nil {
				t.Fatalf("accepted unsupported reference: %s", raw)
			}
		}
	}
}

func TestDecodeResponse_PreserveBoundariesAndNumbers(t *testing.T) {
	body := rpcBody(`{"task":{"id":"task","contextId":"ctx","status":{"state":"TASK_STATE_INPUT_REQUIRED","message":{"messageId":"m","role":"ROLE_AGENT","parts":[{"text":"question"}]}},"history":[{"messageId":"h","role":"ROLE_USER","contextId":"ctx","taskId":"task","referenceTaskIds":["task"],"parts":[{"data":{"n":9007199254740993,"decimal":1.230000000000000001}}]}],"artifacts":[{"artifactId":"a","name":"one","description":"first","parts":[{"text":"a"},{"text":"b"}]},{"artifactId":"b","parts":[{"data":{"nested":[123456789012345678901234567890]}}]}]}}`)
	r, err := DecodeResponse("1.0", Send, "request", 200, body)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "input-required" || len(r.Parts) != 0 || len(r.StatusMessages) != 1 || len(r.History) != 1 || len(r.Artifacts) != 2 || len(r.Artifacts[0].Parts) != 2 {
		t.Fatalf("lost boundaries: %+v", r)
	}
	if r.History[0].Parts[0].Data["n"] != json.Number("9007199254740993") || r.History[0].Parts[0].Data["decimal"] != json.Number("1.230000000000000001") {
		t.Fatal("rounded application numbers")
	}
}

func TestDecodeResponse_RejectMalformed(t *testing.T) {
	for _, body := range []string{
		`null`, `[]`, `{"jsonrpc":"1.0","id":"request","result":{}}`,
		`{"jsonrpc":"2.0","id":"other","result":{}}`,
		`{"jsonrpc":"2.0","id":1,"result":{}}`,
		`{"jsonrpc":"2.0","id":"request","result":{},"error":{"code":1,"message":"bad"}}`,
		`{"jsonrpc":"2.0","id":"request","result":{},"id":"request"}`,
		string(rpcBody(`{"task":null}`)), string(rpcBody(`{"message":null}`)),
		string(rpcBody(`{"task":{},"message":{}}`)),
		string(rpcBody(`{"statusUpdate":{}}`)),
		string(rpcBody(`{"task":{"id":"t","status":{"state":"unknown"}}}`)),
		string(rpcBody(`{"task":{"id":"t","status":{"state":"TASK_STATE_WORKING"},"history":[{"messageId":"m","role":"ROLE_AGENT","taskId":"foreign","parts":[{"text":"x"}]}]}}`)),
		string(rpcBody(`{"task":{"id":"t","status":{"state":"TASK_STATE_WORKING"},"history":[{"messageId":"m","role":"ROLE_AGENT","contextId":"foreign","parts":[{"text":"x"}]}]}}`)),
		string(rpcBody(`{"message":{"messageId":"m","role":"ROLE_AGENT","referenceTaskIds":["foreign"],"parts":[{"text":"x"}]}}`)),
		string(rpcBody(`{"message":{"messageId":"m","role":"agent","parts":[{"text":"x"}]}}`)),
		string(rpcBody(`{"message":{"messageId":"m","role":"ROLE_AGENT","parts":[{"text":null}]}}`)),
		string(rpcBody(`{"message":{"messageId":"m","role":"ROLE_AGENT","parts":[{"text":"a","text":"b"}]}}`)),
		string(rpcBody(`{"message":{"messageId":"m","role":"ROLE_AGENT","parts":[{"text":"a","data":{}}]}}`)),
		string(rpcBody(`{"message":{"messageId":"m","role":"ROLE_AGENT","parts":[{"data":null}]}}`)),
		string(rpcBody(`{"message":{"messageId":"m","role":"ROLE_AGENT","parts":[{"data":[1]}]}}`)),
		string(rpcBody(`{"message":{"messageId":"m","role":"ROLE_AGENT","parts":[{"data":{"id":1,"id":2}}]}}`)),
		string(rpcBody(`{"message":{"messageId":"m","role":"ROLE_AGENT","parts":[{"text":"x"}]}}`)) + `{}`,
	} {
		if _, err := DecodeResponse("1.0", Send, "request", 200, []byte(body)); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	if _, err := DecodeResponse("1.0", Get, "request", 200, rpcBody(`{"message":{"messageId":"m","role":"ROLE_AGENT","parts":[{"text":"x"}]}}`)); err == nil {
		t.Fatal("get accepted message")
	}
	if _, err := DecodeResponse("1.0", Send, "request", 200, []byte(strings.Repeat("x", maxRPCBytes+1))); err == nil {
		t.Fatal("accepted oversized response")
	}
}

func TestDecodeResponse_Errors(t *testing.T) {
	for _, tc := range []struct {
		status, code int
		message      string
		retryable    bool
	}{
		{409, -32054, "Session operation in progress, please retry", true},
		{409, -32054, "Conflict", false},
		{424, -32055, "downstream failure", false},
		{429, -32053, "throttled", true},
		{200, -32603, "internal error", false},
	} {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "request", "error": map[string]any{"code": tc.code, "message": tc.message, "data": map[string]any{"private": "never disclose"}}})
		_, err := DecodeResponse("0.3", Send, "request", tc.status, body)
		var safe *Error
		if !errors.As(err, &safe) || safe.Status != tc.status || safe.Code != tc.code || safe.Retryable != tc.retryable {
			t.Fatalf("wrong classification: %v", err)
		}
		if strings.Contains(err.Error(), tc.message) || strings.Contains(err.Error(), "never disclose") {
			t.Fatal("leaked remote error")
		}
	}
	for _, status := range []int{401, 403, 424, 429} {
		_, err := DecodeResponse("1.0", Get, "request", status, []byte("private remote text"))
		var safe *Error
		if !errors.As(err, &safe) || safe.Status != status || !strings.Contains(err.Error(), fmt.Sprint(status)) || strings.Contains(err.Error(), "private") {
			t.Fatal(err)
		}
	}
}
