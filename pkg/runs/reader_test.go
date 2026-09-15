package runs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	var body []byte
	for i, line := range lines {
		body = append(body, line...)
		if i < len(lines)-1 {
			body = append(body, '\n')
		}
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestQuery_PartialTailAndUnknownSchema(t *testing.T) {
	dir := t.TempDir()
	good := `{"schema_version":1,"recorder_instance_id":"r","sequence":1,"attempt_id":"a1","started_at":"2026-01-01T00:00:00Z","returned_at":"2026-01-01T00:00:01Z","duration_ms":1,"requested_name":"s__t","disposition":"completed","stage":"downstream","reason":"ok"}`
	unknown := `{"schema_version":99,"attempt_id":"a2","disposition":"completed"}`
	malformed := `{not-json`
	secret := `{"schema_version":1,"attempt_id":"leak","disposition":"completed","stage":"downstream","reason":"ok","arguments":{"token":"s3cret"}}`
	path := filepath.Join(dir, activeFileName)
	writeLines(t, path, good, unknown, malformed, secret[:len(secret)/2]) // last line partial

	res, err := Query(context.Background(), dir, Filter{}, 10, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Partial {
		t.Fatal("expected partial result")
	}
	if len(res.Records) != 1 || res.Records[0].AttemptID != "a1" {
		t.Fatalf("records = %+v", res.Records)
	}
	var codes []string
	for _, w := range res.Warnings {
		codes = append(codes, w.Code)
		if w.Message == "" {
			t.Fatal("empty warning message")
		}
		if containsSecret(w.Message) || containsSecret(w.Code) {
			t.Fatalf("warning leaked contents: %+v", w)
		}
	}
	if !containsAll(codes, WarnPartialTail, WarnMalformed, WarnUnsupportedSchema) {
		t.Fatalf("warning codes = %v", codes)
	}
}

func TestQuery_CursorInvalidatedByWipe(t *testing.T) {
	dir := t.TempDir()
	_, err := Query(context.Background(), dir, Filter{}, 10, &Cursor{WipeEpoch: 1}, 2)
	if err == nil {
		t.Fatal("expected cursor invalidation")
	}
}

func TestQuery_FilterAndOrder(t *testing.T) {
	dir := t.TempDir()
	r1 := `{"schema_version":1,"sequence":1,"attempt_id":"a1","returned_at":"2026-01-01T00:00:01Z","requested_name":"s__one","disposition":"completed","stage":"downstream","reason":"ok"}`
	r2 := `{"schema_version":1,"sequence":2,"attempt_id":"a2","returned_at":"2026-01-01T00:00:02Z","requested_name":"s__two","disposition":"denied","stage":"scope","reason":"client_scope"}`
	writeLines(t, filepath.Join(dir, activeFileName), r1, r2, "")
	res, err := Query(context.Background(), dir, Filter{Disposition: DispositionDenied}, 10, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Records) != 1 || res.Records[0].AttemptID != "a2" {
		t.Fatalf("filtered = %+v", res.Records)
	}
	all, err := Query(context.Background(), dir, Filter{}, 10, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Records) != 2 || all.Records[0].AttemptID != "a2" {
		t.Fatalf("order = %+v", all.Records)
	}
}

func TestQuery_Cancellation(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := Query(ctx, dir, Filter{}, 10, nil, 0)
	if err == nil && !res.Partial {
		// empty dir returns before work; still must honor ctx on files.
		_ = os.WriteFile(filepath.Join(dir, activeFileName), []byte("{}\n"), 0o600)
		res, err = Query(ctx, dir, Filter{}, 10, nil, 0)
		if err == nil && !res.Partial {
			t.Fatal("canceled query should error or be partial")
		}
	}
}

func TestQueryPath_Missing(t *testing.T) {
	_, err := QueryPath(context.Background(), filepath.Join(t.TempDir(), "missing.jsonl"), Filter{}, 10, nil)
	if err == nil {
		t.Fatal("expected missing path error")
	}
}

func containsSecret(s string) bool {
	return s == "s3cret" || s == malformedContentsSentinel
}

const malformedContentsSentinel = "{not-json"

func containsAll(have []string, want ...string) bool {
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

func TestOlderThanCursor(t *testing.T) {
	t.Parallel()
	c := Cursor{ReturnedAt: time.Date(2026, 1, 1, 0, 0, 2, 0, time.UTC), Sequence: 2, AttemptID: "a2"}
	older := Record{ReturnedAt: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC), Sequence: 1, AttemptID: "a1"}
	newer := Record{ReturnedAt: time.Date(2026, 1, 1, 0, 0, 3, 0, time.UTC), Sequence: 3, AttemptID: "a3"}
	if !olderThanCursor(older, c) {
		t.Fatal("expected older")
	}
	if olderThanCursor(newer, c) {
		t.Fatal("did not expect newer")
	}
}
