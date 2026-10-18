package reload

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gridctl/gridctl/pkg/logging"
)

// Watcher monitors a stack file for changes and triggers reload.
type Watcher struct {
	path      string
	basenames map[string]struct{}
	onChange  func() error
	logger    *slog.Logger
	debounce  time.Duration
}

// NewWatcher creates a file watcher for the given stack path.
// onChange is called when the file changes (after debouncing).
func NewWatcher(path string, onChange func() error) *Watcher {
	name := filepath.Base(path)
	return &Watcher{
		path:      path,
		basenames: map[string]struct{}{name: {}},
		onChange:  onChange,
		logger:    logging.NewDiscardLogger(),
		debounce:  300 * time.Millisecond,
	}
}

// AddBasename watches another file in the same directory. A non-member
// basename is ignored. Used for stack.override.yaml beside the stack file.
func (w *Watcher) AddBasename(name string) {
	if w == nil || name == "" {
		return
	}
	if w.basenames == nil {
		w.basenames = map[string]struct{}{}
	}
	w.basenames[name] = struct{}{}
}

func (w *Watcher) watches(name string) bool {
	if w == nil || w.basenames == nil {
		return false
	}
	_, ok := w.basenames[name]
	return ok
}

// SetLogger sets the logger for watcher events.
func (w *Watcher) SetLogger(logger *slog.Logger) {
	if logger != nil {
		w.logger = logger
	}
}

// SetDebounce sets the debounce duration for file changes.
func (w *Watcher) SetDebounce(d time.Duration) {
	w.debounce = d
}

// Watch starts watching the file for changes.
// Blocks until context is cancelled.
//
// We watch the parent directory rather than the file directly because most
// editors use atomic saves (write to temp file, then rename). When a file is
// renamed over the watched file, fsnotify loses track of it. Watching the
// directory catches all events including renames.
func (w *Watcher) Watch(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	// Watch the directory containing the file, not the file itself.
	// This handles atomic saves where editors rename temp files over the target.
	dir := filepath.Dir(w.path)

	if err := watcher.Add(dir); err != nil {
		return err
	}

	w.logger.Info("watching for config changes", "path", w.path)

	var debounceTimer *time.Timer
	var debounceChan <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("stopping config watcher")
			return ctx.Err()

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			// Only process events for a watched basename. Delete and rename
			// matter for an override file: removing it reloads the base stack.
			if !w.watches(filepath.Base(event.Name)) {
				continue
			}

			// Write and Create cover direct writes and atomic saves.
			// Remove and Rename cover deleting or replacing the file.
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
				w.logger.Debug("config file changed", "event", event.Op.String())

				// Debounce: reset timer on each change
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.NewTimer(w.debounce)
				debounceChan = debounceTimer.C
			}

		case <-debounceChan:
			w.logger.Info("config change detected, reloading")
			if err := w.onChange(); err != nil {
				w.logger.Error("reload failed", "error", err)
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
