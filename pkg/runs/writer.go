package runs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultSegmentBytes = 8 << 20

const rotatedStampLayout = "2006-01-02T15-04-05.000"

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

	capStopped    bool
	writeErrs     uint64
	syncErrs      uint64
	tailRecovered bool
	pruneFailures uint64
	rotateSeq     uint64
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
	size, recovered, err := recoverJSONLTail(f, info.Size())
	if err != nil {
		_ = f.Close()
		return err
	}
	w.file = f
	w.currentBytes = size
	w.totalBytes = inventoryBytes(w.dir)
	if recovered {
		w.tailRecovered = true
	}
	w.capStopped = w.totalBytes >= w.maxBytes && !w.canReclaimLocked()
	return nil
}

func recoverJSONLTail(f *os.File, size int64) (int64, bool, error) {
	if size == 0 {
		return 0, false, nil
	}
	const maxScan = 1 << 20
	scan := size
	if scan > maxScan {
		scan = maxScan
	}
	buf := make([]byte, scan)
	if _, err := f.ReadAt(buf, size-scan); err != nil && err != io.EOF {
		return size, false, err
	}
	idx := bytes.LastIndexByte(buf, '\n')
	var newSize int64
	if idx < 0 {
		newSize = size - scan
	} else {
		newSize = size - scan + int64(idx) + 1
	}
	if newSize == size {
		return size, false, nil
	}
	if err := f.Truncate(newSize); err != nil {
		return size, false, err
	}
	return newSize, true, nil
}

func (w *Writer) Append(ctx context.Context, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return fmt.Errorf("runs writer: closed")
	}
	need := int64(len(payload) + 1)
	if need > w.maxBytes {
		w.capStopped = true
		return errCapExhausted
	}
	if w.currentBytes+need > w.segmentSize && w.currentBytes > 0 {
		if err := w.rotateLocked(); err != nil {
			w.writeErrs++
			return err
		}
	}
	if err := w.ensureCapacityLocked(need); err != nil {
		w.capStopped = true
		return err
	}
	n, err := w.file.Write(append(payload, '\n'))
	w.currentBytes += int64(n)
	w.totalBytes += int64(n)
	if err != nil {
		w.writeErrs++
		if info, stErr := w.file.Stat(); stErr == nil {
			if size, recovered, recErr := recoverJSONLTail(w.file, info.Size()); recErr == nil {
				delta := w.currentBytes - size
				w.currentBytes = size
				w.totalBytes -= delta
				if w.totalBytes < 0 {
					w.totalBytes = 0
				}
				if recovered {
					w.tailRecovered = true
				}
			}
		}
		return err
	}
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
	ents, err := confinedReadDir(w.dir)
	if err != nil {
		return false
	}
	for _, name := range ents {
		if isRotatedName(name) {
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
	ents, err := confinedReadDir(w.dir)
	if err != nil {
		return err
	}
	var segs []seg
	for _, name := range ents {
		if !isRotatedName(name) {
			continue
		}
		path := filepath.Join(w.dir, name)
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		mod := info.ModTime()
		if ts, ok := rotatedStamp(name); ok && ts.Before(mod) {
			mod = ts
		}
		segs = append(segs, seg{name: name, mod: mod, size: info.Size()})
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
	dest, err := w.uniqueRotatedPathLocked()
	if err != nil {
		return err
	}
	if err := os.Rename(w.path, dest); err != nil {
		return err
	}
	_ = os.Chmod(dest, filePerm)
	w.currentBytes = 0
	return w.openLocked()
}

func (w *Writer) uniqueRotatedPathLocked() (string, error) {
	stamp := time.Now().UTC().Format(rotatedStampLayout)
	for i := 0; i < 10000; i++ {
		w.rotateSeq++
		name := fmt.Sprintf("runs-%s-%d.jsonl", stamp, w.rotateSeq)
		dest := filepath.Join(w.dir, name)
		if _, err := os.Lstat(dest); os.IsNotExist(err) {
			return dest, nil
		}
	}
	return "", fmt.Errorf("runs writer: rotation name collision")
}

func (w *Writer) Sync(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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
	wasOpen := w.file != nil
	err := w.pruneLocked(now)
	if err != nil {
		w.pruneFailures++
	}
	if wasOpen && w.file == nil {
		if openErr := w.openLocked(); openErr != nil {
			w.writeErrs++
			if err == nil {
				err = openErr
			}
		}
	}
	return err
}

func (w *Writer) pruneLocked(now time.Time) error {
	if w.maxAge <= 0 {
		w.totalBytes = inventoryBytes(w.dir)
		return nil
	}
	cutoff := now.Add(-w.maxAge)
	if err := w.ageActiveLocked(cutoff); err != nil {
		return err
	}
	ents, err := confinedReadDir(w.dir)
	if err != nil {
		if os.IsNotExist(err) {
			w.totalBytes = 0
			return nil
		}
		return err
	}
	for _, name := range ents {
		if !isRotatedName(name) {
			continue
		}
		path := filepath.Join(w.dir, name)
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		mod := info.ModTime()
		if ts, ok := rotatedStamp(name); ok && ts.Before(mod) {
			mod = ts
		}
		if mod.Before(cutoff) {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	w.totalBytes = inventoryBytes(w.dir)
	return nil
}

func (w *Writer) ageActiveLocked(cutoff time.Time) error {
	path := w.path
	if path == "" {
		path = filepath.Join(w.dir, activeFileName)
		w.path = path
	}
	if err := refuseSymlink(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	recs, _, _, _, err := readFile(context.Background(), path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return nil
	}
	var keep []Record
	for _, rec := range recs {
		if rec.ReturnedAt.After(cutoff) || rec.ReturnedAt.Equal(cutoff) {
			keep = append(keep, rec)
		}
	}
	if len(keep) == len(recs) {
		return nil
	}
	if w.file != nil {
		_ = w.file.Sync()
		_ = w.file.Close()
		w.file = nil
	}
	tmp := path + ".tmp"
	if err := refuseSymlink(tmp); err != nil && !os.IsNotExist(err) {
		return err
	}
	tf, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, filePerm)
	if err != nil {
		return err
	}
	encOK := true
	for _, rec := range keep {
		raw, err := json.Marshal(rec)
		if err != nil {
			encOK = false
			break
		}
		if _, err := tf.Write(append(raw, '\n')); err != nil {
			encOK = false
			break
		}
	}
	_ = tf.Chmod(filePerm)
	if err := tf.Close(); err != nil {
		encOK = false
	}
	if !encOK {
		_ = os.Remove(tmp)
		return fmt.Errorf("runs writer: active age compact failed")
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Chmod(path, filePerm)
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
	if err := deleteOwned(w.dir); err != nil {
		return fmt.Errorf("runs wipe incomplete: %w", err)
	}
	w.currentBytes = 0
	w.totalBytes = 0
	w.capStopped = false
	w.writtenSeq = 0
	w.syncedSeq = 0
	w.tailRecovered = false
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
		Health:        w.healthLocked(),
		TotalBytes:    w.totalBytes,
		CapStopped:    w.capStopped,
		WriteErrs:     w.writeErrs,
		SyncErrs:      w.syncErrs,
		LastSync:      w.lastSync,
		SyncedSeq:     w.syncedSeq,
		WrittenSeq:    w.writtenSeq,
		TailRecovered: w.tailRecovered,
		PruneFailures: w.pruneFailures,
	}
}

func (w *Writer) healthLocked() string {
	if w.file == nil {
		return HealthStopped
	}
	if w.capStopped || w.writeErrs > 0 || w.syncErrs > 0 || w.pruneFailures > 0 {
		return HealthDegraded
	}
	return HealthOK
}

type writerSnapshot struct {
	Health        string
	TotalBytes    int64
	CapStopped    bool
	WriteErrs     uint64
	SyncErrs      uint64
	LastSync      time.Time
	SyncedSeq     uint64
	WrittenSeq    uint64
	TailRecovered bool
	PruneFailures uint64
}

func rotatedStamp(name string) (time.Time, bool) {
	if name == activeFileName || !strings.HasPrefix(name, "runs-") || !strings.HasSuffix(name, ".jsonl") {
		return time.Time{}, false
	}
	rest := strings.TrimSuffix(strings.TrimPrefix(name, "runs-"), ".jsonl")
	if len(rest) < len(rotatedStampLayout) {
		return time.Time{}, false
	}
	ts, err := time.Parse(rotatedStampLayout, rest[:len(rotatedStampLayout)])
	if err != nil {
		return time.Time{}, false
	}
	leftover := rest[len(rotatedStampLayout):]
	if leftover == "" {
		return ts, true
	}
	if !strings.HasPrefix(leftover, "-") {
		return time.Time{}, false
	}
	if _, err := strconv.ParseUint(leftover[1:], 10, 64); err != nil {
		return time.Time{}, false
	}
	return ts, true
}

func isRotatedName(name string) bool {
	_, ok := rotatedStamp(name)
	return ok
}

func isOwnedName(name string) bool {
	return name == activeFileName || isRotatedName(name) || strings.HasPrefix(name, "runs-") && strings.HasSuffix(name, ".tmp")
}

func confinedReadDir(dir string) ([]string, error) {
	if err := refuseSymlinkParents(dir); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	f, err := openDirNoFollow(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Readdirnames(-1)
}

func inventoryBytes(dir string) int64 {
	ents, err := confinedReadDir(dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, name := range ents {
		if !isOwnedName(name) {
			continue
		}
		info, err := os.Lstat(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
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
	ents, err := confinedReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return Inventory{Stack: stackName, Signal: "runs", Path: path}, nil
		}
		return Inventory{}, err
	}
	inv := Inventory{Stack: stackName, Signal: "runs", Path: path}
	for _, name := range ents {
		if !isOwnedName(name) {
			continue
		}
		info, err := os.Lstat(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
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
	ents, err := confinedReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, name := range ents {
		if !isOwnedName(name) {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
			continue
		}
		names = append(names, path)
	}
	sort.Slice(names, func(i, j int) bool {
		ai := filepath.Base(names[i]) == activeFileName
		aj := filepath.Base(names[j]) == activeFileName
		if ai != aj {
			return !ai
		}
		return names[i] < names[j]
	})
	return names, nil
}
