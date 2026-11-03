package pinapprovals

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateReference_RefusesCredentialShapes(t *testing.T) {
	a2a := "gca2a_t1_" + strings.Repeat("b", 43)
	tests := []struct {
		name    string
		ref     string
		want    error
		message string
	}{
		{
			name:    "url userinfo",
			ref:     "https://user:tok@example.test/x",
			want:    ErrCredentialShaped,
			message: "URL carries userinfo",
		},
		{
			name:    "userinfo without password",
			ref:     "https://user@example.test/x",
			want:    ErrCredentialShaped,
			message: "URL carries userinfo",
		},
		{
			name:    "bearer token",
			ref:     "Bearer abc",
			want:    ErrCredentialShaped,
			message: "looks like a bearer token or key=value secret",
		},
		{
			name:    "authorization header",
			ref:     "Authorization: Basic dXNlcjpwYXNz",
			want:    ErrCredentialShaped,
			message: "looks like a bearer token or key=value secret",
		},
		{
			name:    "key value secret",
			ref:     "token=ghp_xxxxxxxxxxxx",
			want:    ErrCredentialShaped,
			message: "looks like a bearer token or key=value secret",
		},
		{
			name:    "a2a token",
			ref:     a2a,
			want:    ErrCredentialShaped,
			message: "looks like a bearer token or key=value secret",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateReference(tt.ref)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if !strings.Contains(err.Error(), tt.message) || !strings.Contains(err.Error(), "remove it and retry") {
				t.Fatalf("message = %q", err.Error())
			}
			// Refusal is input-only. No file is created by validation.
			if _, statErr := os.Stat(filepath.Join(t.TempDir(), "should-not-exist")); !os.IsNotExist(statErr) {
				t.Fatal("validation touched the filesystem")
			}
		})
	}
}

func TestValidateReference_Bounds(t *testing.T) {
	if err := ValidateReference("   "); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("empty = %v", err)
	}
	long := strings.Repeat("a", maxReferenceBytes+1)
	if err := ValidateReference(long); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("long = %v", err)
	}
	if err := ValidateReference("https://example.test/pr/1"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateReference("plan/review/incident-triage.md"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateReferences_CleansAndBounds(t *testing.T) {
	got, err := ValidateReferences([]string{
		" https://example.test/pr/1 ",
		"https://example.test/pr/1",
		"plan/review/incident-triage.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "https://example.test/pr/1" || got[1] != "plan/review/incident-triage.md" {
		t.Fatalf("cleaned = %#v", got)
	}

	many := make([]string, maxReferences+1)
	for i := range many {
		many[i] = strings.Repeat("a", i+1)
	}
	if _, err := ValidateReferences(many); !errors.Is(err, ErrTooManyReferences) {
		t.Fatalf("too many = %v", err)
	}

	_, err = ValidateReferences([]string{"ok", "Bearer abc"})
	if !errors.Is(err, ErrCredentialShaped) || !strings.Contains(err.Error(), "evidence reference 2") {
		t.Fatalf("indexed = %v", err)
	}
}

func TestValidateReason(t *testing.T) {
	if err := ValidateReason(""); err != nil {
		t.Fatal(err)
	}
	if err := ValidateReason("Adds runbook step 4"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateReason(strings.Repeat("a", maxReasonBytes+1)); !errors.Is(err, ErrReasonTooLong) {
		t.Fatalf("long = %v", err)
	}
	if err := ValidateReason("see token=secret"); !errors.Is(err, ErrCredentialShaped) {
		t.Fatalf("shaped = %v", err)
	}
}

func TestLocalUser_Fallbacks(t *testing.T) {
	t.Run("current user", func(t *testing.T) {
		name, source := localUser(func() (string, error) { return "wcollins", nil }, func(string) string { return "other" })
		if name != "wcollins" || source != SourceOSUser {
			t.Fatalf("got %q %q", name, source)
		}
	})
	t.Run("user then username", func(t *testing.T) {
		env := map[string]string{"USER": "from-user", "USERNAME": "from-username"}
		name, source := localUser(func() (string, error) { return "", errors.New("lookup failed") }, func(k string) string { return env[k] })
		if name != "from-user" || source != SourceOSUser {
			t.Fatalf("USER fallback = %q %q", name, source)
		}
		delete(env, "USER")
		name, source = localUser(func() (string, error) { return "", errors.New("lookup failed") }, func(k string) string { return env[k] })
		if name != "from-username" || source != SourceOSUser {
			t.Fatalf("USERNAME fallback = %q %q", name, source)
		}
	})
	t.Run("none set", func(t *testing.T) {
		name, source := localUser(func() (string, error) { return "", errors.New("lookup failed") }, func(string) string { return "" })
		if name != "" || source != "" {
			t.Fatalf("got %q %q", name, source)
		}
	})
	t.Run("nil lookup does not panic", func(t *testing.T) {
		name, source := localUser(nil, nil)
		if name != "" || source != "" {
			t.Fatalf("got %q %q", name, source)
		}
	})
	// The production wrapper must not panic even when the OS lookup fails.
	t.Run("production wrapper", func(t *testing.T) {
		name, source := LocalUser()
		if name != "" && source != SourceOSUser {
			t.Fatalf("source = %q", source)
		}
	})
}

func TestResolveApprover(t *testing.T) {
	name, source := ResolveApprover("principal-1", "declared-name")
	if name != "principal-1" || source != SourceOAuth {
		t.Fatalf("oauth = %q %q", name, source)
	}
	name, source = ResolveApprover("", "declared-name")
	if name != "declared-name" || source != SourceDeclared {
		t.Fatalf("declared = %q %q", name, source)
	}
}

func TestLog_AppendBoundsAndOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "approvals", "skills", "lab.json")
	log := Open(path, "lab", KindSkill)
	if err := log.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxHistory+1; i++ {
		err := log.Append(t.Context(), "incident-triage", Entry{
			ApprovedAt:   time.Date(2026, 10, i+1, 0, 0, 0, 0, time.UTC),
			ApprovedHash: strings.Repeat("ab", 8) + string(rune('a'+i)),
			Bound:        i%2 == 0,
			Reason:       "step",
			Evidence:     []string{"https://example.test/pr/" + string(rune('a'+i))},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	history := log.History("incident-triage")
	if len(history) != MaxHistory {
		t.Fatalf("len = %d, want %d", len(history), MaxHistory)
	}
	if !history[0].ApprovedAt.After(history[1].ApprovedAt) {
		t.Fatal("history is not newest first")
	}
	latest, ok := log.Latest("incident-triage")
	if !ok || !latest.ApprovedAt.Equal(history[0].ApprovedAt) {
		t.Fatalf("latest = %+v", latest)
	}
	// Copies do not alias the stored evidence slice.
	history[0].Evidence[0] = "mutated"
	again, _ := log.Latest("incident-triage")
	if again.Evidence[0] == "mutated" {
		t.Fatal("History returned a shared evidence slice")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	var doc logFile
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version != FileVersion || doc.Stack != "lab" || doc.Kind != KindSkill {
		t.Fatalf("header = %+v", doc)
	}
	if len(doc.Subjects["incident-triage"]) != MaxHistory {
		t.Fatalf("on disk = %d", len(doc.Subjects["incident-triage"]))
	}
}

func TestLog_CorruptAndNewerVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lab.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	log := Open(path, "lab", KindTool)
	err := log.Load(t.Context())
	if !errors.Is(err, ErrCorrupt) || !strings.Contains(err.Error(), path) {
		t.Fatalf("corrupt = %v", err)
	}
	if _, ok := log.Latest("server"); ok {
		t.Fatal("corrupt load left history")
	}

	newer := `{"version":"2","stack":"lab","kind":"tool","subjects":{}}`
	if err := os.WriteFile(path, []byte(newer), 0o600); err != nil {
		t.Fatal(err)
	}
	err = log.Load(t.Context())
	if !errors.Is(err, ErrNewerVersion) || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), `"2"`) {
		t.Fatalf("newer = %v", err)
	}
	if err := log.Append(t.Context(), "server", Entry{ApprovedHash: "abc"}); err == nil {
		t.Fatal("append after newer-version load should fail")
	}
}

func TestLog_CancellationBeforeIO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lab.json")
	log := Open(path, "lab", KindSkill)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := log.Load(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("load = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("cancelled load created a file")
	}
}

func TestFileVersionAdvertised(t *testing.T) {
	if FileVersion != "1" {
		t.Fatalf("FileVersion = %q", FileVersion)
	}
}
