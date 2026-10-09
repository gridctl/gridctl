package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// import golden files freeze stdout, stderr, and the exit code of the
// servers-only path. The harness replaces the Go test temp root and the
// backup timestamp so a later run can compare without freezing host paths.
var (
	importGoldenMu      sync.Mutex
	importGoldenSeq     = map[string]int{}
	importBackupStampRe = regexp.MustCompile(`\.gridctl-backup-\d{8}-\d{6}`)
	importClockRe       = regexp.MustCompile(`\d{2}:\d{2}:\d{2}`)
)

func checkImportGolden(t *testing.T, home, cwd string, args []string, stdout, stderr string, code int) {
	t.Helper()
	if importArgsSkipGolden(args) || !importGoldenTest(t.Name()) {
		return
	}
	normOut, normErr := normalizeImportGolden(stdout, stderr, home, cwd)
	path := importGoldenPath(t)
	record := os.Getenv("GRIDCTL_RECORD_IMPORT_GOLDEN") == "1"
	body := formatImportGolden(code, normOut, normErr)
	if record {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing import golden %s (set GRIDCTL_RECORD_IMPORT_GOLDEN=1 to capture): %v", path, err)
	}
	if string(want) != body {
		t.Fatalf("import golden mismatch for %s\n--- got normalized ---\n%s\n--- want ---\n%s\n--- raw stdout ---\n%s\n--- raw stderr ---\n%s",
			path, body, string(want), stdout, stderr)
	}
}

func TestImportGolden_DefaultPathExtras(t *testing.T) {
	bin := buildImportBinary(t)
	home := t.TempDir()
	cwd := t.TempDir()

	t.Run("no client scan", func(t *testing.T) {
		stdout, stderr, code := runImportBin(t, bin, home, cwd, "import")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if !strings.Contains(stdout, "No supported LLM clients detected") {
			t.Fatalf("stdout = %s", stdout)
		}
	})

	t.Run("unknown client", func(t *testing.T) {
		_, stderr, code := runImportBin(t, bin, home, cwd, "import", "nosuch")
		if code != 1 || !strings.Contains(stderr, "unknown client \"nosuch\"") || !strings.Contains(stderr, "Supported clients:") {
			t.Fatalf("exit %d stderr=%s", code, stderr)
		}
		if !strings.Contains(stderr, "Error:") {
			t.Fatalf("missing Error: prefix: %s", stderr)
		}
	})
}

func importGoldenTest(name string) bool {
	return strings.HasPrefix(name, "TestImportOpenCode_CLI") ||
		strings.HasPrefix(name, "TestImportProject_CLI") ||
		strings.HasPrefix(name, "TestImportGolden_")
}

func importArgsSkipGolden(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func importGoldenPath(t *testing.T) string {
	t.Helper()
	importGoldenMu.Lock()
	n := importGoldenSeq[t.Name()]
	importGoldenSeq[t.Name()] = n + 1
	importGoldenMu.Unlock()
	name := strings.NewReplacer("/", "__", " ", "_", ":", "_").Replace(t.Name())
	_, file, _, ok := runtime.Caller(0)
	dir := "testdata/import_golden"
	if ok {
		dir = filepath.Join(filepath.Dir(file), "testdata", "import_golden")
	}
	return filepath.Join(dir, fmt.Sprintf("%s__%d.txt", name, n))
}

func formatImportGolden(code int, stdout, stderr string) string {
	return fmt.Sprintf("exit: %d\n--- stdout\n%s--- stderr\n%s", code, stdout, stderr)
}

func normalizeImportGolden(stdout, stderr, home, cwd string) (string, string) {
	root := importGoldenTempRoot(home, cwd)
	fix := func(s string) string {
		if root != "" {
			s = strings.ReplaceAll(s, root, "<TESTROOT>")
		} else {
			if home != "" {
				s = strings.ReplaceAll(s, home, "<HOME>")
			}
			if cwd != "" {
				s = strings.ReplaceAll(s, cwd, "<CWD>")
			}
		}
		s = importBackupStampRe.ReplaceAllString(s, ".gridctl-backup-TIMESTAMP")
		return importClockRe.ReplaceAllString(s, "<TIME>")
	}
	return fix(stdout), fix(stderr)
}

// importGoldenTempRoot is the testing.T temp directory that owns home and
// cwd. Replacing it stabilizes every per-test path those fixtures create.
func importGoldenTempRoot(paths ...string) string {
	tmp := os.TempDir()
	for _, p := range paths {
		if p == "" || !strings.HasPrefix(p, tmp) {
			continue
		}
		dir := p
		for {
			base := filepath.Base(dir)
			parent := filepath.Dir(dir)
			if strings.HasPrefix(base, "Test") && strings.HasPrefix(dir, tmp) {
				return dir
			}
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return ""
}
