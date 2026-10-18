package stackedit

import (
	"strings"
	"testing"
)

func TestSetServerEnabled_InsertsAndRemoves(t *testing.T) {
	source := []byte("name: demo\nmcp-servers:\n  - name: one\n    command: [/bin/true]\n    # keep me\n  - name: two\n    url: https://example.com\n")
	updated, changed, err := SetServerEnabled(source, "one", false)
	if err != nil || !changed {
		t.Fatalf("disable: changed=%v err=%v", changed, err)
	}
	if !strings.Contains(string(updated), "enabled: false") {
		t.Fatalf("body = %s", updated)
	}
	if !strings.Contains(string(updated), "# keep me") {
		t.Fatal("comment was dropped")
	}
	again, changed, err := SetServerEnabled(updated, "one", false)
	if err != nil || changed {
		t.Fatalf("second disable: changed=%v err=%v", changed, err)
	}
	if string(again) != string(updated) {
		t.Fatal("idempotent disable rewrote the file")
	}
	enabled, changed, err := SetServerEnabled(updated, "one", true)
	if err != nil || !changed {
		t.Fatalf("enable: changed=%v err=%v", changed, err)
	}
	if strings.Contains(string(enabled), "enabled:") {
		t.Fatalf("enabled key remained: %s", enabled)
	}
	_, _, err = SetServerEnabled(source, "missing", false)
	if err == nil {
		t.Fatal("expected unknown server")
	}
}

func TestSetOverrideEnabled_CreatesAndPreserves(t *testing.T) {
	updated, changed, err := SetOverrideEnabled(nil, "demo", false)
	if err != nil || !changed {
		t.Fatalf("create: changed=%v err=%v body=%s", changed, err, updated)
	}
	if !strings.Contains(string(updated), "enabled: false") {
		t.Fatalf("body = %s", updated)
	}
	withOther := []byte("mcp-servers:\n  other:\n    replicas: 2\n")
	updated, changed, err = SetOverrideEnabled(withOther, "demo", true)
	if err != nil || !changed {
		t.Fatal(err)
	}
	body := string(updated)
	if !strings.Contains(body, "replicas: 2") || !strings.Contains(body, "enabled: true") {
		t.Fatalf("body = %s", body)
	}
}
