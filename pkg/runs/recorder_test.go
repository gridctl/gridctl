package runs

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func waitRecords(t *testing.T, dir string, n int) QueryResult {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last QueryResult
	for time.Now().Before(deadline) {
		res, err := Query(context.Background(), dir, Filter{}, 100, nil, 0)
		if err == nil && len(res.Records) >= n {
			return res
		}
		if err == nil {
			last = res
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d records, last=%d", n, len(last.Records))
	return last
}

func TestRecorder_WritesFinalRecord(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRecorder(Config{
		Enabled: true, Dir: dir, MaxBytes: 1 << 20, MaxAge: time.Hour,
		QueueSize: 16, SyncInterval: 20 * time.Millisecond, ShutdownDrain: time.Second,
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	a := r.Begin(context.Background(), "github__create_issue")
	a.SetResolved("github", "create_issue", 0)
	a.SetOutcome(DispositionCompleted, StageDownstream, ReasonOK)
	a.SetLabels("claude-code", "ops")
	a.Finish()

	res := waitRecords(t, dir, 1)
	rec := res.Records[0]
	if rec.RequestedName != "github__create_issue" {
		t.Fatalf("requested = %q", rec.RequestedName)
	}
	if rec.ResolvedServer != "github" || rec.ResolvedTool != "create_issue" {
		t.Fatalf("resolved = %s/%s", rec.ResolvedServer, rec.ResolvedTool)
	}
	if rec.Disposition != DispositionCompleted {
		t.Fatalf("disposition = %s", rec.Disposition)
	}
	if rec.ClientLabel != "claude-code" {
		t.Fatalf("client label = %q", rec.ClientLabel)
	}
	if rec.DurationMS < 0 {
		t.Fatal("negative duration")
	}
}

func TestRecorder_DisabledNoSideEffects(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRecorder(Config{
		Enabled: false, Dir: dir, MaxBytes: 1 << 20, MaxAge: time.Hour,
		QueueSize: 8, SyncInterval: time.Second, ShutdownDrain: time.Millisecond,
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	a := r.Begin(context.Background(), "s__t")
	a.SetOutcome(DispositionCompleted, StageDownstream, ReasonOK)
	a.Finish()
	time.Sleep(50 * time.Millisecond)
	ents, _ := os.ReadDir(dir)
	if len(ents) != 0 {
		t.Fatalf("disabled recorder wrote files: %v", ents)
	}
}

func TestRecorder_OmitLabels(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRecorder(Config{
		Enabled: true, OmitLabels: true, Dir: dir, MaxBytes: 1 << 20, MaxAge: time.Hour,
		QueueSize: 8, SyncInterval: 20 * time.Millisecond, ShutdownDrain: time.Second,
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	a := r.Begin(context.Background(), "s__t")
	a.SetLabels("secret-client", "secret-access")
	a.SetOutcome(DispositionCompleted, StageDownstream, ReasonOK)
	a.Finish()
	res := waitRecords(t, dir, 1)
	if res.Records[0].ClientLabel != "" || res.Records[0].AccessLabel != "" {
		t.Fatalf("labels present despite omit: %+v", res.Records[0])
	}
}

func TestRecorder_SecretsNeverQueued(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRecorder(Config{
		Enabled: true, Dir: dir, MaxBytes: 1 << 20, MaxAge: time.Hour,
		QueueSize: 8, SyncInterval: 20 * time.Millisecond, ShutdownDrain: time.Second,
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	secret := "s3cret-token-value"
	a := r.Begin(context.Background(), "s__t")
	a.SetPreviousAttemptID(secret)
	a.SetTraceID("https://" + secret)
	a.SetOutcome(DispositionToolError, StageDownstream, ReasonToolError)
	a.Finish()
	res := waitRecords(t, dir, 1)
	raw, _ := json.Marshal(res.Records[0])
	if strings.Contains(string(raw), secret) {
		t.Fatal("secret leaked into record")
	}
	body, _ := os.ReadFile(filepath.Join(dir, activeFileName))
	if strings.Contains(string(body), secret) {
		t.Fatal("secret leaked into file")
	}
}

func TestRecorder_ParentChildCorrelation(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRecorder(Config{
		Enabled: true, Dir: dir, MaxBytes: 1 << 20, MaxAge: time.Hour,
		QueueSize: 8, SyncInterval: 20 * time.Millisecond, ShutdownDrain: time.Second,
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	parent := r.Begin(context.Background(), "execute")
	parent.SetOutcome(DispositionCompleted, StageCodeMode, ReasonCodeMode)
	child := r.Begin(parent.Context(), "inner__tool")
	child.SetOutcome(DispositionToolError, StageDownstream, ReasonToolError)
	child.Finish()
	parent.Finish()
	res := waitRecords(t, dir, 2)
	var inner, outer Record
	for _, rec := range res.Records {
		switch rec.RequestedName {
		case "inner__tool":
			inner = rec
		case "execute":
			outer = rec
		}
	}
	if inner.ParentAttemptID != outer.AttemptID {
		t.Fatalf("parent %s, child parent %s", outer.AttemptID, inner.ParentAttemptID)
	}
	if inner.RootAttemptID != outer.AttemptID && inner.RootAttemptID != outer.RootAttemptID {
		t.Fatalf("root mismatch inner=%s outer=%s", inner.RootAttemptID, outer.AttemptID)
	}
}

func TestRecorder_QueueFullDrops(t *testing.T) {
	r := &Recorder{
		queue:    make(chan queuedEvent, 1),
		lastWarn: map[string]time.Time{},
		logger:   discardLogger(),
		omit:     false,
	}
	r.enabled.Store(true)
	r.gen.Store(1)
	r.instanceID = "test"
	r.queue <- queuedEvent{gen: 1, payload: []byte("{}")}
	a := r.Begin(context.Background(), "s__t")
	a.SetOutcome(DispositionCompleted, StageDownstream, ReasonOK)
	a.Finish()
	if r.drops.queueFull.Load() != 1 {
		t.Fatalf("drops = %d, want 1", r.drops.queueFull.Load())
	}
}

func TestRecorder_GenerationMismatch(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRecorder(Config{
		Enabled: true, Dir: dir, MaxBytes: 1 << 20, MaxAge: time.Hour,
		QueueSize: 8, SyncInterval: 20 * time.Millisecond, ShutdownDrain: time.Second,
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	a := r.Begin(context.Background(), "old__tool")
	if err := r.Apply(Config{Enabled: true, Dir: t.TempDir(), MaxBytes: 1 << 20, MaxAge: time.Hour}); err != nil {
		t.Fatal(err)
	}
	a.SetOutcome(DispositionCompleted, StageDownstream, ReasonOK)
	a.Finish()
	time.Sleep(80 * time.Millisecond)
	if r.drops.generation.Load() == 0 {
		t.Fatal("expected generation drop for old-stack event")
	}
}

func TestRecorder_WipeBarrier(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRecorder(Config{
		Enabled: true, Dir: dir, MaxBytes: 1 << 20, MaxAge: time.Hour,
		QueueSize: 32, SyncInterval: 20 * time.Millisecond, ShutdownDrain: time.Second,
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	RegisterActive("wipe-test", r)
	defer UnregisterActive("wipe-test")

	a := r.Begin(context.Background(), "pre__wipe")
	if err := r.Wipe(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.SetOutcome(DispositionCompleted, StageDownstream, ReasonOK)
	a.Finish()

	post := r.Begin(context.Background(), "post__wipe")
	post.SetOutcome(DispositionCompleted, StageDownstream, ReasonOK)
	post.Finish()
	res := waitRecords(t, dir, 1)
	for _, rec := range res.Records {
		if rec.RequestedName == "pre__wipe" {
			t.Fatal("pre-wipe record resurrected")
		}
	}
	if res.Records[0].RequestedName != "post__wipe" {
		t.Fatalf("got %q, want post__wipe", res.Records[0].RequestedName)
	}
}

func TestRecorder_StatusIndependent(t *testing.T) {
	r, err := NewRecorder(Config{
		Enabled: true, Dir: t.TempDir(), MaxBytes: 1 << 20, MaxAge: time.Hour,
		QueueSize: 4, SyncInterval: time.Second, ShutdownDrain: time.Millisecond,
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	st := r.Status()
	if !st.Enabled || st.HistoricalLoss != LossUnknown {
		t.Fatalf("status = %+v", st)
	}
	if st.WriterHealth == "" {
		t.Fatal("missing writer health")
	}
}

func TestOfflineWipeRefusesActiveWriter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GRIDCTL_HOME", "")
	r, err := NewRecorder(Config{
		Enabled: true, StackName: "live-stack", MaxBytes: 1 << 20, MaxAge: time.Hour,
		QueueSize: 4, SyncInterval: time.Second, ShutdownDrain: time.Millisecond,
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	RegisterActive("live-stack", r)
	defer UnregisterActive("live-stack")
	if err := WipeStack(context.Background(), "live-stack"); err != nil {
		t.Fatalf("live wipe via registry should succeed: %v", err)
	}
}

func TestOfflineWipeRefusesForeignDaemon(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GRIDCTL_HOME", "")
	if err := state.Save(&state.DaemonState{
		StackName: "foreign",
		PID:       os.Getpid(),
		Port:      1,
		StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := WipeStack(context.Background(), "foreign"); err != ErrActiveWriter {
		t.Fatalf("got %v, want ErrActiveWriter", err)
	}
}

func TestNewRecorder_OpenFailureStaysEnabledDegraded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := NewRecorder(Config{
		Enabled: true, Dir: path, MaxBytes: 1 << 20, MaxAge: time.Hour,
		QueueSize: 4, SyncInterval: time.Second, ShutdownDrain: time.Millisecond,
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	st := r.Status()
	if !st.Enabled || st.Effective || st.WriterHealth != HealthDegraded {
		t.Fatalf("status = %+v", st)
	}
	if st.Failures[DropOpenError] == 0 && st.Drops[DropOpenError] == 0 {
		t.Fatal("expected open failure accounting")
	}
}

func TestRecorder_WipeConcurrentWithFinish(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRecorder(Config{
		Enabled: true, Dir: dir, MaxBytes: 1 << 20, MaxAge: time.Hour,
		QueueSize: 64, SyncInterval: 20 * time.Millisecond, ShutdownDrain: time.Second,
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 20; i++ {
		// Bind every attempt to the old epoch before racing Finish with Wipe.
		a := r.Begin(context.Background(), "pre__wipe")
		a.SetOutcome(DispositionCompleted, StageDownstream, ReasonOK)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			a.Finish()
		}()
	}
	var wipeErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		wipeErr = r.Wipe(context.Background())
	}()
	close(start)
	wg.Wait()
	if wipeErr != nil {
		t.Fatal(wipeErr)
	}
	post := r.Begin(context.Background(), "post__wipe")
	post.SetOutcome(DispositionCompleted, StageDownstream, ReasonOK)
	post.Finish()
	res := waitRecords(t, dir, 1)
	for _, rec := range res.Records {
		if rec.RequestedName == "pre__wipe" {
			t.Fatal("pre-wipe record resurrected")
		}
	}
	found := false
	for _, rec := range res.Records {
		if rec.RequestedName == "post__wipe" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing post-wipe record")
	}
}

func TestDeleteOwnedRefusesSymlinkedStackDir(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outside, activeFileName)
	if err := os.WriteFile(victim, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(root, "runs")
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "demo")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := deleteOwned(link); err == nil {
		t.Fatal("expected symlink directory refusal")
	}
	body, err := os.ReadFile(victim)
	if err != nil || string(body) != "keep\n" {
		t.Fatalf("unrelated file modified: %v %q", err, body)
	}
}

func TestRecorder_EntryTimeOmitLabels(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRecorder(Config{
		Enabled: true, Dir: dir, MaxBytes: 1 << 20, MaxAge: time.Hour,
		QueueSize: 8, SyncInterval: 20 * time.Millisecond, ShutdownDrain: time.Second,
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	a := r.Begin(context.Background(), "s__t")
	a.SetLabels("keep-me", "keep-me")
	if err := r.Apply(Config{Enabled: true, Dir: dir, OmitLabels: true, MaxBytes: 1 << 20, MaxAge: time.Hour}); err != nil {
		t.Fatal(err)
	}
	a.SetOutcome(DispositionCompleted, StageDownstream, ReasonOK)
	a.Finish()
	res := waitRecords(t, dir, 1)
	if res.Records[0].ClientLabel != "keep-me" {
		t.Fatalf("entry-time labels lost: %+v", res.Records[0])
	}
}
