//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/secreport"
)

func TestSecurityReport_FileAndSnapshotModes(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(stackPath, []byte("version: \"1\"\nname: demo\nmcp-servers:\n  - name: fetch\n    image: example/fetch:1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := secreport.Load(context.Background(), secreport.SourceRef{Kind: secreport.SourceFile, Value: stackPath}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.UnknownCount == 0 {
		t.Fatal("expected unknown optional evidence")
	}
	var b strings.Builder
	if err := secreport.WriteJSON(&b, report); err != nil {
		t.Fatal(err)
	}
	snapPath := filepath.Join(dir, "report.json")
	if err := os.WriteFile(snapPath, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := secreport.Load(context.Background(), secreport.SourceRef{Kind: secreport.SourceSnapshot, Value: snapPath}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Source.Historical {
		t.Fatal("snapshot must remain historical")
	}
}
