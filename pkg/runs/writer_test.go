package runs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriter_AppendSyncPermissions(t *testing.T) {
	dir := t.TempDir()
	w, err := newWriter(WriterConfig{Dir: dir, MaxBytes: 4096, MaxAge: time.Hour, SegmentSize: 2048})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	payload, _ := json.Marshal(Record{
		SchemaVersion: SchemaVersion,
		AttemptID:     "a1",
		Disposition:   DispositionCompleted,
		Stage:         StageDownstream,
		Reason:        ReasonOK,
		Sequence:      1,
	})
	if err := w.Append(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	if err := w.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	snap := w.snapshot()
	if snap.SyncedSeq != 1 {
		t.Fatalf("synced seq = %d, want 1", snap.SyncedSeq)
	}

	info, err := os.Stat(filepath.Join(dir, activeFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != filePerm {
		t.Fatalf("file perm = %o, want %o", info.Mode().Perm(), filePerm)
	}
	dinfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dinfo.Mode().Perm() != dirPerm {
		t.Fatalf("dir perm = %o, want %o", dinfo.Mode().Perm(), dirPerm)
	}
}

func TestWriter_CapExhaustedStopsAppend(t *testing.T) {
	dir := t.TempDir()
	w, err := newWriter(WriterConfig{Dir: dir, MaxBytes: 80, MaxAge: time.Hour, SegmentSize: 40})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	payload := []byte(`{"schema_version":1,"attempt_id":"x","disposition":"completed","stage":"downstream","reason":"ok"}`)
	var sawCap bool
	for i := 0; i < 20; i++ {
		err := w.Append(context.Background(), payload)
		if err == errCapExhausted {
			sawCap = true
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if !sawCap {
		t.Fatal("expected capacity exhaustion")
	}
	snap := w.snapshot()
	if snap.TotalBytes > 80 {
		t.Fatalf("total bytes %d exceeded budget 80", snap.TotalBytes)
	}
}

func TestWriter_RotateAndAgePrune(t *testing.T) {
	dir := t.TempDir()
	w, err := newWriter(WriterConfig{Dir: dir, MaxBytes: 10 << 20, MaxAge: time.Hour, SegmentSize: 60})
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"schema_version":1,"attempt_id":"x","disposition":"completed","stage":"downstream","reason":"ok"}`)
	for i := 0; i < 6; i++ {
		if err := w.Append(context.Background(), payload); err != nil {
			t.Fatal(err)
		}
	}
	_ = w.Close()

	ents, _ := os.ReadDir(dir)
	var rotated int
	for _, e := range ents {
		if isRotatedName(e.Name()) {
			rotated++
			old := time.Now().Add(-2 * time.Hour)
			_ = os.Chtimes(filepath.Join(dir, e.Name()), old, old)
		}
	}
	if rotated == 0 {
		t.Fatal("expected rotated segment")
	}

	w2, err := newWriter(WriterConfig{Dir: dir, MaxBytes: 10 << 20, MaxAge: time.Hour, SegmentSize: 60})
	if err != nil {
		t.Fatal(err)
	}
	defer w2.Close()
	ents, _ = os.ReadDir(dir)
	for _, e := range ents {
		if isRotatedName(e.Name()) {
			t.Fatalf("rotated segment %s should have been age-pruned", e.Name())
		}
	}
}

func TestWriter_SustainedAppendAtBudget(t *testing.T) {
	dir := t.TempDir()
	w, err := newWriter(WriterConfig{Dir: dir, MaxBytes: 400, MaxAge: time.Hour, SegmentSize: 400})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	payload := []byte(`{"schema_version":1,"attempt_id":"x","disposition":"completed","stage":"downstream","reason":"ok"}`)
	var wrote int
	for i := 0; i < 40; i++ {
		err := w.Append(context.Background(), payload)
		if err == errCapExhausted {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		wrote++
	}
	if wrote < 5 {
		t.Fatalf("wrote %d records, want rotation/reclaim to accept more than one segment", wrote)
	}
	snap := w.snapshot()
	if snap.TotalBytes > 400 {
		t.Fatalf("total bytes %d exceeded budget", snap.TotalBytes)
	}
}

func TestWriter_RapidRotationPreservesRecords(t *testing.T) {
	dir := t.TempDir()
	w, err := newWriter(WriterConfig{Dir: dir, MaxBytes: 10 << 20, MaxAge: time.Hour, SegmentSize: 80})
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"schema_version":1,"attempt_id":"x","disposition":"completed","stage":"downstream","reason":"ok"}`)
	const n = 8
	for i := 0; i < n; i++ {
		if err := w.Append(context.Background(), payload); err != nil {
			t.Fatal(err)
		}
	}
	_ = w.Close()
	res, err := Query(context.Background(), dir, Filter{}, 100, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Records) != n {
		t.Fatalf("records = %d, want %d (rotation must not overwrite)", len(res.Records), n)
	}
}

func TestWriter_RecoversPartialTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, activeFileName)
	now := time.Now().UTC().Format(time.RFC3339)
	good := `{"schema_version":1,"attempt_id":"a1","disposition":"completed","stage":"downstream","reason":"ok","returned_at":"` + now + `"}` + "\n"
	if err := os.WriteFile(path, []byte(good+"{\"partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := newWriter(WriterConfig{Dir: dir, MaxBytes: 4096, MaxAge: 24 * time.Hour, SegmentSize: 2048})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	payload := []byte(`{"schema_version":1,"attempt_id":"a2","disposition":"completed","stage":"downstream","reason":"ok","returned_at":"` + now + `"}`)
	if err := w.Append(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	res, err := Query(context.Background(), dir, Filter{}, 10, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Records) != 2 {
		t.Fatalf("records = %+v", res.Records)
	}
}

func TestWriter_AgePrunesActiveFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, activeFileName)
	freshAt := time.Now().UTC().Format(time.RFC3339)
	old := `{"schema_version":1,"sequence":1,"attempt_id":"old","returned_at":"2020-01-01T00:00:00Z","disposition":"completed","stage":"downstream","reason":"ok"}`
	fresh := `{"schema_version":1,"sequence":2,"attempt_id":"new","returned_at":"` + freshAt + `","disposition":"completed","stage":"downstream","reason":"ok"}`
	if err := os.WriteFile(path, []byte(old+"\n"+fresh+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := newWriter(WriterConfig{Dir: dir, MaxBytes: 4096, MaxAge: 24 * time.Hour, SegmentSize: 2048})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	res, err := Query(context.Background(), dir, Filter{}, 10, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Records) != 1 || res.Records[0].AttemptID != "new" {
		t.Fatalf("records = %+v", res.Records)
	}
}

func TestWriter_RefuseSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	link := filepath.Join(dir, "link")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := newWriter(WriterConfig{Dir: link, MaxBytes: 1024, MaxAge: time.Hour}); err == nil {
		t.Fatal("expected symlink refusal")
	}
}
