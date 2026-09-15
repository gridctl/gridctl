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
