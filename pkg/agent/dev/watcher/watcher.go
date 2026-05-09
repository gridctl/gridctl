// Package watcher recursively watches a project directory for typed-skill
// source changes and pushes events to subscribers. It extends the
// fsnotify pattern from pkg/reload/watcher.go with recursive
// directory descent and a per-extension filter so the IDE wakes only
// when a `.go` or `.ts` skill file actually changes.
//
// Subscribers receive coalesced events: each directory is tracked
// separately, but rapid bursts (editor save followed by formatter
// followed by lint) collapse into one notification per debounce
// window. The intent is that the IDE refetches the AST exactly once
// per logical edit even when the editor stages five filesystem
// operations to make it happen.
package watcher

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Event is one coalesced change notification. The path is absolute;
// the lang is "go" or "ts" — empty when the change does not match
// either extension (still surfaced because the IDE may want to
// invalidate its skill cache for non-source files like SKILL.md).
type Event struct {
	// Path is the absolute on-disk path of the file that changed.
	Path string `json:"path"`

	// Lang is "go", "ts", or empty for SKILL.md / agent.json edits.
	Lang string `json:"lang,omitempty"`

	// Op is the fsnotify Op string ("WRITE", "CREATE", "REMOVE")
	// for diagnostics. The IDE renders no UI off this — Subscribers
	// react identically to all change kinds.
	Op string `json:"op"`

	// Time is the wall-clock time the event was coalesced.
	Time time.Time `json:"time"`
}

// defaultDebounce is the coalescing window. 200ms is enough to merge
// editor-stage bursts (write → tmp-rename → lint) into one event,
// short enough that the IDE's refetch is invisibly fast on save.
const defaultDebounce = 200 * time.Millisecond

// Watcher streams filesystem events to subscribers. One Watcher
// covers one project root and survives transient subdirectory churn
// (newly-created directories are added on the fly).
type Watcher struct {
	root     string
	debounce time.Duration
	logger   *slog.Logger

	mu          sync.Mutex
	subscribers map[chan Event]struct{}
}

// New constructs a Watcher rooted at projectRoot. The constructor
// only validates the root exists; Run starts the actual fsnotify
// goroutine.
func New(projectRoot string) (*Watcher, error) {
	if projectRoot == "" {
		return nil, errors.New("watcher: project root is required")
	}
	info, err := os.Stat(projectRoot)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("watcher: project root is not a directory")
	}
	return &Watcher{
		root:        projectRoot,
		debounce:    defaultDebounce,
		logger:      slog.Default(),
		subscribers: make(map[chan Event]struct{}),
	}, nil
}

// SetLogger overrides the slog.Logger watcher events emit on. Nil
// clears the logger so the watcher runs silently — useful in tests.
func (w *Watcher) SetLogger(l *slog.Logger) {
	if l == nil {
		w.logger = slog.New(slog.NewTextHandler(noopWriter{}, nil))
		return
	}
	w.logger = l
}

// SetDebounce overrides the coalescing window. Values <= 0 fall back
// to the default.
func (w *Watcher) SetDebounce(d time.Duration) {
	if d <= 0 {
		w.debounce = defaultDebounce
		return
	}
	w.debounce = d
}

// Subscribe registers a new channel that receives Events. The
// returned cleanup function unsubscribes and closes the channel; it
// is safe to call from any goroutine. Subscribers MUST drain or
// close their channel — Run uses a non-blocking send so a slow
// reader silently drops events rather than stalling the whole
// dispatcher.
func (w *Watcher) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 8)
	w.mu.Lock()
	w.subscribers[ch] = struct{}{}
	w.mu.Unlock()
	return ch, func() {
		w.mu.Lock()
		if _, ok := w.subscribers[ch]; ok {
			delete(w.subscribers, ch)
			close(ch)
		}
		w.mu.Unlock()
	}
}

// Run starts the watch loop and blocks until ctx is done. Returns
// nil on graceful cancellation; error otherwise.
func (w *Watcher) Run(ctx context.Context) error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fsw.Close()

	if err := w.addRecursive(fsw, w.root); err != nil {
		return err
	}
	w.logger.Info("agent dev watcher started", "root", w.root)

	var pending = make(map[string]Event)
	var debounce *time.Timer
	var debounceCh <-chan time.Time

	flush := func() {
		w.mu.Lock()
		subs := make([]chan Event, 0, len(w.subscribers))
		for ch := range w.subscribers {
			subs = append(subs, ch)
		}
		w.mu.Unlock()
		for _, ev := range pending {
			for _, ch := range subs {
				select {
				case ch <- ev:
				default:
					// Drop on full channel — the subscriber is
					// slower than the source. Better to lose a
					// notification than block every other watcher.
				}
			}
		}
		pending = make(map[string]Event)
	}

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("agent dev watcher stopping")
			return ctx.Err()

		case ev, ok := <-fsw.Events:
			if !ok {
				return nil
			}
			if ev.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					_ = w.addRecursive(fsw, ev.Name)
				}
			}
			lang := classifyExt(ev.Name)
			if lang == "" && filepath.Base(ev.Name) != "SKILL.md" && filepath.Base(ev.Name) != "agent.json" {
				continue
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			pending[ev.Name] = Event{
				Path: ev.Name,
				Lang: lang,
				Op:   ev.Op.String(),
				Time: time.Now().UTC(),
			}
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.NewTimer(w.debounce)
			debounceCh = debounce.C

		case <-debounceCh:
			flush()
			debounceCh = nil

		case err, ok := <-fsw.Errors:
			if !ok {
				return nil
			}
			w.logger.Warn("agent dev watcher error", "error", err)
		}
	}
}

// addRecursive walks dir and adds every subdirectory to the
// fsnotify watcher. We stop at well-known noise sources
// (node_modules, .git, dist, build) so we don't run out of inotify
// handles on large projects.
func (w *Watcher) addRecursive(fsw *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable subtrees rather than aborting
		}
		if !d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if isSkipDir(base) {
			return filepath.SkipDir
		}
		return fsw.Add(path)
	})
}

// classifyExt returns "go" or "ts" for recognised typed-skill
// source extensions; empty otherwise.
func classifyExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".ts", ".tsx", ".mts":
		return "ts"
	}
	return ""
}

// isSkipDir returns true for directory names we never descend into.
// Avoids exhausting inotify handles on large repos.
func isSkipDir(name string) bool {
	switch name {
	case "node_modules", ".git", "dist", "build", "vendor", ".cache":
		return true
	}
	return false
}

// noopWriter discards everything written to it; used by SetLogger(nil).
type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }
