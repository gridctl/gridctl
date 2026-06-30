package reload

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gridctl/gridctl/pkg/logging"
)

// DirWatcher monitors a directory tree for changes and triggers a debounced
// callback. Unlike Watcher, which targets a single file, DirWatcher watches a
// whole subtree (e.g. the registry skills directory, where each skill is its
// own directory containing SKILL.md plus optional supporting files).
//
// fsnotify is not recursive, so the watcher adds the root and every existing
// subdirectory, and adds newly-created subdirectories as it observes them. The
// callback is expected to re-read the tree from disk (a full reload), so the
// watcher does not need to capture every individual event — it only needs to
// fire shortly after a burst of changes settles.
type DirWatcher struct {
	root     string
	onChange func() error
	logger   *slog.Logger
	debounce time.Duration
}

// NewDirWatcher creates a recursive directory watcher rooted at root.
// onChange is called (after debouncing) whenever an entry under root is
// created, written, removed, or renamed.
func NewDirWatcher(root string, onChange func() error) *DirWatcher {
	return &DirWatcher{
		root:     root,
		onChange: onChange,
		logger:   logging.NewDiscardLogger(),
		debounce: 300 * time.Millisecond,
	}
}

// SetLogger sets the logger for watcher events.
func (w *DirWatcher) SetLogger(logger *slog.Logger) {
	if logger != nil {
		w.logger = logger
	}
}

// SetDebounce sets the debounce duration for change events.
func (w *DirWatcher) SetDebounce(d time.Duration) {
	w.debounce = d
}

// Watch starts watching the directory tree for changes. It blocks until the
// context is cancelled.
//
// The root may not exist yet at startup (the registry skills directory is only
// created when the first skill is saved). Watch creates it so there is always a
// stable directory to watch, sidestepping the need to re-arm when it appears.
func (w *DirWatcher) Watch(ctx context.Context) error {
	if err := os.MkdirAll(w.root, 0o755); err != nil {
		return err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	w.addTree(watcher, w.root)
	w.logger.Info("watching directory for changes", "path", w.root)

	var debounceTimer *time.Timer
	var debounceChan <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("stopping directory watcher", "path", w.root)
			return ctx.Err()

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			// Keep the watch set current: when a new subdirectory appears
			// (e.g. a freshly-created skill directory), start watching it so
			// later writes inside it are observed too.
			if event.Op&fsnotify.Create != 0 {
				if info, statErr := os.Stat(event.Name); statErr == nil && info.IsDir() {
					w.addTree(watcher, event.Name)
				}
			}

			// Reload on any structural change. Chmod is intentionally ignored
			// as pure-metadata noise.
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) != 0 {
				w.logger.Debug("registry change detected", "event", event.Op.String(), "path", event.Name)
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.NewTimer(w.debounce)
				debounceChan = debounceTimer.C
			}

		case <-debounceChan:
			w.logger.Info("registry change detected, refreshing", "path", w.root)
			if err := w.onChange(); err != nil {
				w.logger.Error("registry refresh failed", "error", err)
			}
			debounceChan = nil

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			w.logger.Error("watcher error", "error", err)
		}
	}
}

// addTree adds dir and all of its existing subdirectories to the watcher.
// Failures are logged and skipped so one unreadable subdirectory does not
// disable watching for the rest of the tree.
func (w *DirWatcher) addTree(watcher *fsnotify.Watcher, dir string) {
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			w.logger.Debug("walk error while adding watch, skipping", "path", path, "error", err)
			return nil //nolint:nilerr // skip this entry, keep walking the rest of the tree
		}
		if !d.IsDir() {
			return nil
		}
		if addErr := watcher.Add(path); addErr != nil {
			w.logger.Debug("failed to watch directory, skipping", "path", path, "error", addErr)
		}
		return nil
	})
}
