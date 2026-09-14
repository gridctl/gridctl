package secreport_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gridctl/gridctl/pkg/secreport"
)

func TestExampleFixturesLoad(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "examples", "security-evidence")
	report, err := secreport.Load(context.Background(), secreport.SourceRef{Kind: secreport.SourceFile, Value: filepath.Join(root, "stack.yaml")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.UnknownCount == 0 {
		t.Fatal("file mode should leave optional producers unknown")
	}
	data, err := os.ReadFile(filepath.Join(root, "snapshot-partial.json"))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := secreport.ParseSnapshot(data)
	if err != nil {
		t.Fatal(err)
	}
	if snap.FailCount == 0 {
		t.Fatal("expected a failing check in the partial snapshot")
	}
}
