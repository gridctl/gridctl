package runs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultSegmentBytes = 8 << 20

// Writer appends schema-versioned JSONL records with rotation, a total
// logical-byte budget, periodic sync, and coordinated reopen for wipe.
type Writer struct {
	mu sync.Mutex

	dir         string
	path        string
	file        *os.File
	segmentSize int64
	maxBytes    int64
	maxAge      time.Duration

	currentBytes int64
	totalBytes   int64

	writtenSeq uint64
	syncedSeq  uint64
	lastSync   time.Time

	capStopped bool
	writeErrs  uint64
	syncErrs   uint64
}

// WriterConfig bounds on-disk retention. MaxBytes is the total logical
// record-byte budget across the active file and closed segments.
type WriterConfig struct {
	Dir         string
	MaxBytes    int64
	MaxAge      time.Duration
	SegmentSize int64
}

func newWriter(cfg WriterConfig) (*Writer, error) {
	if cfg.Dir == "" {
		return nil, fmt.Errorf("runs writer: directory is required")
	}
	if cfg.MaxBytes <= 0 {
		return nil, fmt.Errorf("runs writer: max bytes must be positive")
	}
	if cfg.SegmentSize <= 0 {
		cfg.SegmentSize = defaultSegmentBytes
	}
	if cfg.SegmentSize > cfg.MaxBytes {
		cfg.SegmentSize = cfg.MaxBytes
	}
	if err := refuseSymlinkParents(cfg.Dir); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := ensureDir(cfg.Dir); err != nil {
		return nil, err
	}
	w := &Writer{
		dir:         cfg.Dir,
		path:        filepath.Join(cfg.Dir, activeFileName),
		segmentSize: cfg.SegmentSize,
		maxBytes:    cfg.MaxBytes,
		maxAge:      cfg.MaxAge,
	}
	if err := w.pruneLocked(time.Now()); err != nil {
		return nil, err
	}
	if err := w.openLocked(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) openLocked() error {
	if w.file != nil {
		return nil
	}
	if err := refuseSymlinkParents(w.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Lstat(w.path); err == nil {
		if err := refuseSymlink(w.path); err != nil {
			return err
		}
	}
	f, err := openAppendFile(w.path)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.file = f
	w.currentBytes = info.Size()
	w.totalBytes = inventoryBytes(w.dir)
	w.capStopped = w.totalBytes >= w.maxBytes && !w.canReclaimLocked()
	return nil
}

func (w *Writer) Append(_ context.Context, payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return fmt.Errorf("runs writer: closed")
	}
	need := int64(len(payload) + 1)
	if err := w.ensureCapacityLocked(need); err != nil {
		w.capStopped = true
		return err
	}
	if w.currentBytes+need > w.segmentSize && w.currentBytes > 0 {
		if err := w.rotateLocked(); err != nil {
			w.writeErrs++
			return err
		}
	}
	n, err := w.file.Write(append(payload, '\n'))
	if err != nil {
		w.writeErrs++
		return err
	}
	w.currentBytes += int64(n)
	w.totalBytes += int64(n)
	w.writtenSeq++
	return nil
}

func (w *Writer) ensureCapacityLocked(need int64) error {
	if w.totalBytes+need <= w.maxBytes {
		w.capStopped = false
		return nil
	}
	if err := w.reclaimLocked(need); err != nil {
		return err
	}
	if w.totalBytes+need > w.maxBytes {
		return errCapExhausted
	}
	w.capStopped = false
	return nil
}

var errCapExhausted = fmt.Errorf("runs writer: capacity exhausted")

func (w *Writer) canReclaimLocked() bool {
	ents, err := os.ReadDir(w.dir)
	if err != nil {
		return false
	}
	for _, ent := range ents {
		if ent.IsDir() {
			continue
		}
		if isRotatedName(ent.Name()) {
			return true
		}
	}
	return false
}

func (w *Writer) reclaimLocked(need int64) error {
	type seg struct {
		name string
		mod  time.Time
		size int64
	}
	ents, err := os.ReadDir(w.dir)
	if err != nil {
		return err
	}
	var segs []seg
	for _, ent := range ents {
		if ent.IsDir() || !isRotatedName(ent.Name()) {
			continue
		}
		info, err := ent.Info()
		if err != nil {
			continue
		}
		segs = append(segs, seg{name: ent.Name(), mod: info.ModTime(), size: info.Size()})
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].mod.Before(segs[j].mod) })
	for _, s := range segs {
		if w.totalBytes+need <= w.maxBytes {
			break
		}
		path := filepath.Join(w.dir, s.name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		w.totalBytes -= s.size
		if w.totalBytes < 0 {
			w.totalBytes = 0
		}
	}
	return nil
}

func (w *Writer) rotateLocked() error {
	if w.file == nil {
		return nil
	}
	if err := w.file.Sync(); err != nil {
		w.syncErrs++
		return err
	}
	w.syncedSeq = w.writtenSeq
	w.lastSync = time.Now().UTC()
	if err := w.file.Close(); err != nil {
		return err
	}
	w.file = nil
	stamp := time.Now().UTC().Format("2006-01-02T15-04-05.000")
	dest := filepath.Join(w.dir, "runs-"+stamp+".jsonl")
	if err := os.Rename(w.path, dest); err != nil {
		return err
	}
	_ = os.Chmod(dest, filePerm)
	w.currentBytes = 0
	return w.openLocked()
}

func (w *Writer) Sync(_ context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return fmt.Errorf("runs writer: closed")
	}
	if err := w.file.Sync(); err != nil {
		w.syncErrs++
		return err
	}
	w.syncedSeq = w.writtenSeq
	w.lastSync = time.Now().UTC()
	return nil
}

func (w *Writer) Prune(now time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.pruneLocked(now)
}

func (w *Writer) pruneLocked(now time.Time) error {
	if w.maxAge <= 0 {
		w.totalBytes = inventoryBytes(w.dir)
		return nil
	}
	cutoff := now.Add(-w.maxAge)
	ents, err := os.ReadDir(w.dir)
	if err != nil {
		if os.IsNotExist(err) {
			w.totalBytes = 0
			return nil
		}
		return err
	}
	for _, ent := range ents {
		if ent.IsDir() || !isRotatedName(ent.Name()) {
			continue
		}
		info, err := ent.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			path := filepath.Join(w.dir, ent.Name())
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	w.totalBytes = inventoryBytes(w.dir)
	return nil
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	_ = w.file.Sync()
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *Writer) wipeAndReopen() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	ents, err := os.ReadDir(w.dir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var errs []error
	for _, ent := range ents {
		if ent.IsDir() {
			continue
		}
		if !isOwnedName(ent.Name()) {
			continue
		}
		path := filepath.Join(w.dir, ent.Name())
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	w.currentBytes = 0
	w.totalBytes = 0
	w.capStopped = false
	w.writtenSeq = 0
	w.syncedSeq = 0
	if len(errs) > 0 {
		return fmt.Errorf("runs wipe incomplete: %v", errs)
	}
	return w.openLocked()
}

func (w *Writer) setRetention(maxBytes int64, maxAge time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if maxBytes > 0 {
		w.maxBytes = maxBytes
	}
	if maxAge > 0 {
		w.maxAge = maxAge
	}
}

func (w *Writer) snapshot() writerSnapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	return writerSnapshot{
		Health:     w.healthLocked(),
		TotalBytes: w.totalBytes,
		CapStopped: w.capStopped,
		WriteErrs:  w.writeErrs,
		SyncErrs:   w.syncErrs,
		LastSync:   w.lastSync,
		SyncedSeq:  w.syncedSeq,
		WrittenSeq: w.writtenSeq,
	}
}

func (w *Writer) healthLocked() string {
	if w.file == nil {
		return HealthStopped
	}
	if w.capStopped || w.writeErrs > 0 || w.syncErrs > 0 {
		return HealthDegraded
	}
	return HealthOK
}

type writerSnapshot struct {
	Health     string
	TotalBytes int64
	CapStopped bool
	WriteErrs  uint64
	SyncErrs   uint64
	LastSync   time.Time
	SyncedSeq  uint64
	WrittenSeq uint64
}

func isRotatedName(name string) bool {
	if name == activeFileName {
		return false
	}
	if !strings.HasPrefix(name, "runs-") || !strings.HasSuffix(name, ".jsonl") {
		return false
	}
	ts := strings.TrimSuffix(strings.TrimPrefix(name, "runs-"), ".jsonl")
	_, err := time.Parse("2006-01-02T15-04-05.000", ts)
	return err == nil
}

func isOwnedName(name string) bool {
	return name == activeFileName || isRotatedName(name) || strings.HasPrefix(name, "runs-") && strings.HasSuffix(name, ".tmp")
}

func inventoryBytes(dir string) int64 {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, ent := range ents {
		if ent.IsDir() || !isOwnedName(ent.Name()) {
			continue
		}
		info, err := ent.Info()
		if err != nil {
			continue
		}
		total += info.Size()
	}
	return total
}

// Inventory describes the on-disk footprint of one stack's run records.
type Inventory struct {
	Stack      string    `json:"stack"`
	Signal     string    `json:"signal"`
	Path       string    `json:"path"`
	SizeBytes  int64     `json:"sizeBytes"`
	OldestTime time.Time `json:"oldestTime"`
	NewestTime time.Time `json:"newestTime"`
	FileCount  int       `json:"fileCount"`
}

// StackInventory walks owned run files for a stack. A missing directory
// returns a zero Inventory without error.
func StackInventory(stackName string) (Inventory, error) {
	dir, err := Dir(stackName)
	if err != nil {
		return Inventory{}, err
	}
	path := filepath.Join(dir, activeFileName)
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return Inventory{Stack: stackName, Signal: "runs", Path: path}, nil
		}
		return Inventory{}, err
	}
	inv := Inventory{Stack: stackName, Signal: "runs", Path: path}
	for _, ent := range ents {
		if ent.IsDir() || !isOwnedName(ent.Name()) {
			continue
		}
		info, err := ent.Info()
		if err != nil {
			continue
		}
		inv.SizeBytes += info.Size()
		inv.FileCount++
		mt := info.ModTime()
		if inv.OldestTime.IsZero() || mt.Before(inv.OldestTime) {
			inv.OldestTime = mt
		}
		if mt.After(inv.NewestTime) {
			inv.NewestTime = mt
		}
	}
	return inv, nil
}

func listOwnedFiles(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, ent := range ents {
		if ent.IsDir() || !isOwnedName(ent.Name()) {
			continue
		}
		names = append(names, filepath.Join(dir, ent.Name()))
	}
	sort.Slice(names, func(i, j int) bool {
		// Oldest closed segments first; active file last.
		ai := filepath.Base(names[i]) == activeFileName
		aj := filepath.Base(names[j]) == activeFileName
		if ai != aj {
			return !ai
		}
		return names[i] < names[j]
	})
	return names, nil
}
