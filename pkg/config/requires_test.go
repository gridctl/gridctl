package config

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestCheckRequires(t *testing.T) {
	t.Cleanup(ResetRunningVersion)

	cases := []struct {
		name       string
		running    string
		constraint string
		wantErr    string
	}{
		{name: "omitted", running: "1.0.0", constraint: "", wantErr: ""},
		{name: "whitespace omitted", running: "1.0.0", constraint: "  ", wantErr: ""},
		{name: "rc satisfies lower floor", running: "1.0.0-rc.2", constraint: ">=0.1.0", wantErr: ""},
		{name: "rc fails release floor", running: "1.0.0-rc.2", constraint: ">=1.0.0", wantErr: "stack requires gridctl >=1.0.0, running 1.0.0-rc.2 (gridctl upgrade --version v1.0.0)"},
		{name: "v prefix running version", running: "v1.0.0-rc.2", constraint: ">=1.0.0", wantErr: "stack requires gridctl >=1.0.0, running 1.0.0-rc.2 (gridctl upgrade --version v1.0.0)"},
		{name: "exact floor passes", running: "1.1.0", constraint: ">=1.1.0", wantErr: ""},
		{name: "prerelease floor passes", running: "1.1.0-rc.1", constraint: ">=1.1.0-rc.1", wantErr: ""},
		{name: "unsatisfied simple floor", running: "1.0.0", constraint: ">=1.1.0", wantErr: "stack requires gridctl >=1.1.0, running 1.0.0 (gridctl upgrade --version v1.1.0)"},
		{name: "spaced floor", running: "1.0.0", constraint: ">= 1.1.0", wantErr: "stack requires gridctl >= 1.1.0, running 1.0.0 (gridctl upgrade --version v1.1.0)"},
		{name: "and range hints the highest floor", running: "1.5.0", constraint: ">=1.0.0, <1.2.0", wantErr: "stack requires gridctl >=1.0.0, <1.2.0, running 1.5.0 (gridctl upgrade --version v1.0.0)"},
		{name: "merged floors hint the highest", running: "1.0.0", constraint: ">=0.1.0, >=1.1.0", wantErr: "stack requires gridctl >=0.1.0, >=1.1.0, running 1.0.0 (gridctl upgrade --version v1.1.0)"},
		{name: "or range has no hint", running: "0.5.0", constraint: ">=1.0.0 || >=2.0.0", wantErr: "stack requires gridctl >=1.0.0 || >=2.0.0, running 0.5.0"},
		{name: "and whose floor is excluded has no hint", running: "1.2.0", constraint: ">=1.0.0, >1.5.0", wantErr: "stack requires gridctl >=1.0.0, >1.5.0, running 1.2.0"},
		{name: "git describe dirty is enforced", running: "v1.0.0-rc.2-31-ga4c8f63-dirty", constraint: ">=1.0.0", wantErr: "stack requires gridctl >=1.0.0, running 1.0.0-rc.2-31-ga4c8f63-dirty (gridctl upgrade --version v1.0.0)"},
		{name: "git describe dirty satisfies rc floor", running: "v1.0.0-rc.2-31-ga4c8f63-dirty", constraint: ">=1.0.0-rc.2", wantErr: ""},
		{name: "invalid string", running: "1.0.0", constraint: "not a constraint", wantErr: `must be a semver constraint (e.g. ">=1.1.0")`},
		{name: "invalid string on dev", running: "dev", constraint: "not a constraint", wantErr: `must be a semver constraint (e.g. ">=1.1.0")`},
		{name: "dev skips satisfaction", running: "dev", constraint: ">=9.0.0", wantErr: ""},
		{name: "empty version skips", running: "", constraint: ">=9.0.0", wantErr: ""},
		{name: "garbage version skips", running: "nightly", constraint: ">=9.0.0", wantErr: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ResetRunningVersion()
			SetRunningVersion(tc.running)
			err := CheckRequires(tc.constraint)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("CheckRequires() = %v", err)
				}
				return
			}
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("CheckRequires() = %v, want %q", err, tc.wantErr)
			}
			var re *RequiresError
			if !errorAs(err, &re) {
				t.Fatalf("error type = %T", err)
			}
		})
	}
}

func TestCheckRequires_DevWarnsOnce(t *testing.T) {
	ResetRunningVersion()
	t.Cleanup(ResetRunningVersion)
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	SetRunningVersion("dev")
	if err := CheckRequires(">=1.1.0"); err != nil {
		t.Fatal(err)
	}
	if err := CheckRequires(">=9.0.0"); err != nil {
		t.Fatal(err)
	}
	logs := buf.String()
	if strings.Count(logs, "requires.gridctl not enforced") != 1 {
		t.Fatalf("warn count, logs = %s", logs)
	}
	var rec struct {
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(logs)), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Msg != `requires.gridctl not enforced: running version "dev" is not a release` {
		t.Fatalf("msg = %q", rec.Msg)
	}
}

func TestValidate_RequiresField(t *testing.T) {
	ResetRunningVersion()
	t.Cleanup(ResetRunningVersion)
	SetRunningVersion("1.0.0")

	stack := &Stack{
		Name:     "lab",
		Network:  Network{Name: "lab-net"},
		Requires: &RequiresConfig{Gridctl: ">=1.1.0"},
	}
	err := Validate(stack)
	if err == nil || !strings.Contains(err.Error(), `requires.gridctl: stack requires gridctl >=1.1.0, running 1.0.0 (gridctl upgrade --version v1.1.0)`) {
		t.Fatalf("Validate() = %v", err)
	}
	result := ValidateWithIssues(stack)
	if result.ErrorCount != 1 || result.Valid {
		t.Fatalf("result = %+v", result)
	}
	found := false
	for _, issue := range result.Issues {
		if issue.Field == "requires.gridctl" && issue.Severity == SeverityError &&
			issue.Message == "stack requires gridctl >=1.1.0, running 1.0.0 (gridctl upgrade --version v1.1.0)" {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues = %+v", result.Issues)
	}

	SetRunningVersion("1.1.0")
	if err := Validate(&Stack{Name: "lab", Network: Network{Name: "lab-net"}, Requires: &RequiresConfig{Gridctl: ">=1.1.0"}}); err != nil {
		t.Fatalf("satisfied Validate() = %v", err)
	}
}

func TestDecodeStack_RequiresUnknownKey(t *testing.T) {
	_, err := DecodeStack([]byte("name: lab\nrequires:\n  gridctl: \">=1.1.0\"\n  extra: true\n"))
	if err == nil || !strings.Contains(err.Error(), "extra") {
		t.Fatalf("DecodeStack() = %v", err)
	}
}

func TestIntersectConstraints(t *testing.T) {
	cases := []struct {
		name, child, parent, want string
	}{
		{name: "simple floors", child: ">=1.1.0", parent: ">=1.0.0", want: ">=1.1.0, >=1.0.0"},
		{name: "or on child", child: ">=1.0.0 || >=2.0.0", parent: ">=1.5.0", want: ">=1.0.0, >=1.5.0 || >=2.0.0, >=1.5.0"},
		{name: "or on parent", child: ">=1.0.0", parent: ">=1.5.0 || >=3.0.0", want: ">=1.0.0, >=1.5.0 || >=1.0.0, >=3.0.0"},
		{name: "or on both", child: ">=1.0.0 || >=2.0.0", parent: ">=1.5.0 || >=3.0.0", want: ">=1.0.0, >=1.5.0 || >=1.0.0, >=3.0.0 || >=2.0.0, >=1.5.0 || >=2.0.0, >=3.0.0"},
		{name: "and inside or", child: ">=1.0.0, <2.0.0 || >=3.0.0", parent: ">=1.5.0", want: ">=1.0.0, <2.0.0, >=1.5.0 || >=3.0.0, >=1.5.0"},
		{name: "invalid child stays invalid", child: "not a constraint", parent: ">=1.0.0", want: "not a constraint, >=1.0.0"},
		{name: "trailing or stays invalid", child: ">=1.0.0 ||", parent: ">=1.5.0", want: ">=1.0.0 ||, >=1.5.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := intersectConstraints(tc.child, tc.parent); got != tc.want {
				t.Fatalf("intersectConstraints() = %q, want %q", got, tc.want)
			}
		})
	}

	// A version that satisfies only the child's first OR clause must fail
	// the intersection. 1.6.0 satisfies the child and the parent.
	merged := intersectConstraints(">=1.0.0 || >=2.0.0", ">=1.5.0")
	if err := checkRequires("1.2.0", merged); err == nil {
		t.Fatal("1.2.0 satisfied a parent floor it misses")
	}
	if err := checkRequires("1.6.0", merged); err != nil {
		t.Fatal(err)
	}
	if err := checkRequires("2.1.0", merged); err != nil {
		t.Fatal(err)
	}

	// Composing a second merge must keep every ancestor floor.
	once := intersectConstraints(">=1.5.0", ">=1.0.0 || >=2.0.0")
	twice := intersectConstraints(">=1.6.0", once)
	if err := checkRequires("1.2.0", twice); err == nil {
		t.Fatalf("1.2.0 satisfied %q", twice)
	}
	if err := checkRequires("1.7.0", twice); err != nil {
		t.Fatalf("%q: %v", twice, err)
	}
}

func errorAs(err error, target **RequiresError) bool {
	re, ok := err.(*RequiresError)
	if !ok {
		return false
	}
	*target = re
	return true
}
