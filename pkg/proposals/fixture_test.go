package proposals

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/ctxpins"
	"github.com/gridctl/gridctl/pkg/hookpins"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skillpins"
	"github.com/gridctl/gridctl/pkg/skills"
)

const testStack = "lab"

// fixture is one isolated home with a real registry, context store, pin
// stores, and proposal store.
type fixture struct {
	t        *testing.T
	home     string
	regDir   string
	pinsDir  string
	lockPath string
	reg      *registry.Store
	sp       *skillpins.Store
	mgr      *contexts.Manager
	cp       *ctxpins.Store
	hp       *hookpins.Store
	store    *Store
	targets  *Targets
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	f := &fixture{
		t:        t,
		home:     home,
		regDir:   filepath.Join(home, ".gridctl", "registry"),
		pinsDir:  filepath.Join(home, ".gridctl", "pins"),
		lockPath: filepath.Join(home, ".gridctl", "skills.lock.yaml"),
	}
	f.reg = registry.NewStore(f.regDir)
	f.sp = skillpins.NewWithPath(f.pinsDir, testStack)
	if err := f.sp.Load(); err != nil {
		t.Fatal(err)
	}
	f.mgr = contexts.NewManagerWithHome(home)
	f.cp = ctxpins.NewWithPath(filepath.Join(f.pinsDir, "context"))
	f.hp = hookpins.NewWithPath(f.pinsDir)
	if err := f.hp.Load(); err != nil {
		t.Fatal(err)
	}
	f.store = NewWithPath(filepath.Join(home, ".gridctl", "proposals"))
	f.targets = &Targets{
		Registry:    f.reg,
		SkillPins:   f.sp,
		Stack:       testStack,
		LockPath:    f.lockPath,
		Context:     f.mgr,
		ContextPins: f.cp,
		HookPins:    f.hp,
	}
	return f
}

func (f *fixture) ctx() context.Context { return f.t.Context() }

func (f *fixture) addSkill(name, body string, files map[string]string) {
	f.t.Helper()
	if err := f.reg.SaveSkill(&registry.AgentSkill{
		Name:        name,
		Description: "Triage incidents for " + name,
		Body:        body,
		State:       registry.StateActive,
	}); err != nil {
		f.t.Fatal(err)
	}
	for p, data := range files {
		if err := f.reg.WriteFile(name, p, []byte(data)); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *fixture) skillDir(name string) string {
	return filepath.Join(f.regDir, "skills", name)
}

func (f *fixture) readSkillFile(name, p string) string {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.skillDir(name), filepath.FromSlash(p)))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(data)
}

func (f *fixture) pinSkills() {
	f.t.Helper()
	if _, err := f.sp.Sync(f.reg); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) currentHash(kind, name string) string {
	f.t.Helper()
	target, err := f.targets.Resolve(f.ctx(), kind, name)
	if err != nil {
		f.t.Fatal(err)
	}
	h, err := target.CurrentHash(f.ctx())
	if err != nil {
		f.t.Fatal(err)
	}
	return h
}

// skillDiff edits SKILL.md by replacing old with new in the raw file.
func (f *fixture) skillDiff(name, old, replacement string) string {
	f.t.Helper()
	before := f.readSkillFile(name, "SKILL.md")
	after := strings.Replace(before, old, replacement, 1)
	if after == before {
		f.t.Fatalf("%q not found in SKILL.md", old)
	}
	return genDiff(f.t, "SKILL.md", before, after, false)
}

func (f *fixture) create(req CreateRequest) *Proposal {
	f.t.Helper()
	p, err := f.store.Create(f.ctx(), req, f.targets)
	if err != nil {
		f.t.Fatalf("create: %v", err)
	}
	return p
}

func skillRequest(name, base, diff string) CreateRequest {
	return CreateRequest{
		Kind:           KindSkill,
		Target:         name,
		BaseHash:       base,
		Diff:           diff,
		Classification: ClassKnowledge,
		Prevalence:     Prevalence{Sessions: 14, Runs: 6},
		Evidence:       []string{"session:abc123", "run:42"},
		Reason:         "seen in 14 sessions",
		Source:         "dream",
		CreatedBy:      "curator",
	}
}

func approver() ApproveRequest {
	return ApproveRequest{ApprovedBy: "reviewer", ApprovedBySource: "declared"}
}

func (f *fixture) writeLock(lf *skills.LockFile) {
	f.t.Helper()
	if err := skills.WriteLockFile(f.lockPath, lf); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) addHook(name string) {
	f.t.Helper()
	dir := filepath.Join(f.regDir, "hooks", name)
	writeFile(f.t, filepath.Join(dir, "hook.json"), `{"event":"PreToolUse","command":"./check.sh"}`+"\n")
	writeFile(f.t, filepath.Join(dir, "check.sh"), "#!/bin/sh\nexit 0\n")
	if err := f.hp.Pin(f.ctx(), hookpins.PinRequest{Name: name, Dir: dir, Source: hookpins.SourceGit}); err != nil {
		f.t.Fatal(err)
	}
	if err := f.hp.Load(); err != nil {
		f.t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readOrEmpty returns a file's bytes, or "<absent>" when it does not exist.
func readOrEmpty(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "<absent>"
		}
		t.Fatal(err)
	}
	return string(data)
}

// treeDigest hashes every file under dir with its relative path, so a test
// can assert a directory is byte-identical.
func treeDigest(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		sum := sha256.Sum256(data)
		lines = append(lines, rel+" "+hex.EncodeToString(sum[:]))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func (f *fixture) skillPinFile() string {
	return filepath.Join(f.pinsDir, testStack+".skills.json")
}

func (f *fixture) skillLogFile() string {
	return filepath.Join(f.pinsDir, "approvals", "skills", testStack+".json")
}

func (f *fixture) proposalFile(id string) string {
	return filepath.Join(f.home, ".gridctl", "proposals", id+".json")
}

// resolverFunc adapts a function to Resolver for fault injection.
type resolverFunc func(ctx context.Context, kind, name string) (Target, error)

func (r resolverFunc) Resolve(ctx context.Context, kind, name string) (Target, error) {
	return r(ctx, kind, name)
}
