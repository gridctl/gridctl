package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/runs"
)

func TestEmitRunsResultJSON(t *testing.T) {
	result := runs.QueryResult{
		Records: []runs.Record{{
			SchemaVersion: 1,
			AttemptID:     "a1",
			Disposition:   runs.DispositionCompleted,
			RequestedName: "s__t",
		}},
		Warnings: []runs.Warning{},
	}
	stdout, stderr := captureOutput(t, func() error {
		return emitRunsResult("json", result)
	})
	if !strings.Contains(stdout, `"attempt_id": "a1"`) && !strings.Contains(stdout, `"attempt_id":"a1"`) {
		t.Fatalf("stdout = %s", stdout)
	}
	if strings.Contains(stdout, "secret") {
		t.Fatal("secret in json")
	}
	_ = stderr
}

func TestEmitRunsResultPartialExit(t *testing.T) {
	// Partial is reported on stderr; json stdout stays structured.
	result := runs.QueryResult{
		Records:  []runs.Record{},
		Partial:  true,
		Warnings: []runs.Warning{{Code: runs.WarnMalformed, Count: 1, Message: "malformed records omitted"}},
	}
	var stdout bytes.Buffer
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := json.NewEncoder(&stdout).Encode(result)
	_ = w.Close()
	os.Stdout = orig
	_, _ = r.Read(make([]byte, 1))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Warnings[0].Message, "{") {
		t.Fatal("warning leaked contents")
	}
}

func TestRunsFilterOfflineFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runs.jsonl")
	line := `{"schema_version":1,"sequence":1,"attempt_id":"a1","returned_at":"2026-01-01T00:00:01Z","requested_name":"s__t","disposition":"completed","stage":"downstream","reason":"ok"}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := runs.QueryPath(context.Background(), path, runs.Filter{}, 10, nil)
	if err != nil || len(res.Records) != 1 {
		t.Fatalf("query path: %v %+v", err, res)
	}
}

func captureOutput(t *testing.T, fn func() error) (string, string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	ro, wo, _ := os.Pipe()
	re, we, _ := os.Pipe()
	os.Stdout, os.Stderr = wo, we
	err := fn()
	_ = wo.Close()
	_ = we.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	if err != nil {
		t.Fatal(err)
	}
	var outBuf, errBuf bytes.Buffer
	_, _ = outBuf.ReadFrom(ro)
	_, _ = errBuf.ReadFrom(re)
	return outBuf.String(), errBuf.String()
}
