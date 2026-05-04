package telemetry

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
)

// signals lists the three telemetry signal types in display order. Inventory
// records and the wipe entry-point both validate against this list so unknown
// signal names never silently leak through.
var signals = []string{"logs", "metrics", "traces"}

// InventoryRecord describes the on-disk footprint of a single (server, signal)
// pair. Sizes and times aggregate the active jsonl plus any rotated/compressed
// lumberjack siblings (e.g. `logs.jsonl`, `logs-2026-04-30T12-00-00.000.jsonl`,
// `logs-…jsonl.gz`). Records are only emitted when at least one matching file
// exists so the wipe modal can drive its enumeration without filtering empties.
type InventoryRecord struct {
	Server     string    `json:"server"`
	Signal     string    `json:"signal"`
	Path       string    `json:"path"`
	SizeBytes  int64     `json:"sizeBytes"`
	OldestTime time.Time `json:"oldestTime"`
	NewestTime time.Time `json:"newestTime"`
	FileCount  int       `json:"fileCount"`
}

// Inventory walks ~/.gridctl/telemetry/<stackName>/ and returns one record per
// (server, signal) pair where at least one file exists. When serverName is
// non-empty only that server's records are returned.
//
// Records are sorted by server name then by the canonical signal order
// (logs, metrics, traces) so the API response is deterministic. A missing
// stack directory returns an empty slice without error — the daemon may
// legitimately have no persisted telemetry yet.
func Inventory(stackName, serverName string) ([]InventoryRecord, error) {
	if stackName == "" {
		return []InventoryRecord{}, nil
	}
	stackDir := filepath.Join(state.TelemetryDir(), stackName)
	entries, err := os.ReadDir(stackDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []InventoryRecord{}, nil
		}
		return nil, fmt.Errorf("read telemetry dir: %w", err)
	}

	records := make([]InventoryRecord, 0, len(entries)*len(signals))
	for _, srv := range entries {
		if !srv.IsDir() {
			continue
		}
		if serverName != "" && srv.Name() != serverName {
			continue
		}
		srvRecords, err := inventoryServer(stackDir, srv.Name())
		if err != nil {
			return nil, err
		}
		records = append(records, srvRecords...)
	}

	sort.SliceStable(records, func(i, j int) bool {
		if records[i].Server != records[j].Server {
			return records[i].Server < records[j].Server
		}
		return signalOrder(records[i].Signal) < signalOrder(records[j].Signal)
	})
	return records, nil
}

func inventoryServer(stackDir, serverName string) ([]InventoryRecord, error) {
	srvDir := filepath.Join(stackDir, serverName)
	files, err := os.ReadDir(srvDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read server telemetry dir: %w", err)
	}

	bySignal := make(map[string]*InventoryRecord, len(signals))
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		sig := matchSignal(f.Name())
		if sig == "" {
			continue
		}
		info, err := f.Info()
		if err != nil {
			continue
		}
		rec, ok := bySignal[sig]
		if !ok {
			rec = &InventoryRecord{
				Server: serverName,
				Signal: sig,
				Path:   filepath.Join(srvDir, sig+".jsonl"),
			}
			bySignal[sig] = rec
		}
		rec.SizeBytes += info.Size()
		rec.FileCount++
		mt := info.ModTime()
		if rec.OldestTime.IsZero() || mt.Before(rec.OldestTime) {
			rec.OldestTime = mt
		}
		if mt.After(rec.NewestTime) {
			rec.NewestTime = mt
		}
	}

	out := make([]InventoryRecord, 0, len(bySignal))
	for _, sig := range signals {
		if rec, ok := bySignal[sig]; ok {
			out = append(out, *rec)
		}
	}
	return out, nil
}

// rotatedTimestamp matches lumberjack's rotated-file timestamp segment exactly:
// YYYY-MM-DDTHH-MM-SS.fff. Anchoring to the timestamp shape keeps a
// user-created `logs-backup.jsonl` from being claimed as a rotation and
// (more importantly) deleted by Wipe.
var rotatedTimestamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}\.\d{3}$`)

// matchSignal returns "logs", "metrics", or "traces" when name matches the
// active jsonl or one of lumberjack's rotated variants for that signal. Empty
// string for any other filename so callers can skip it.
//
// Lumberjack v2 names rotated files <base>-<timestamp>.<ext>, optionally
// followed by .gz when Compress is true. With our active filename
// `<sig>.jsonl`, base = "<sig>" and ext = "jsonl"; rotated siblings look
// like `<sig>-2026-04-30T12-00-00.000.jsonl[.gz]`.
func matchSignal(name string) string {
	for _, sig := range signals {
		if name == sig+".jsonl" {
			return sig
		}
		// Strip optional .gz then the .jsonl suffix; anchor the remaining
		// stem to "<sig>-<timestamp>".
		stem := name
		if suffix, ok := stripSuffix(stem, ".gz"); ok {
			stem = suffix
		}
		stem, ok := stripSuffix(stem, ".jsonl")
		if !ok {
			continue
		}
		ts, ok := stripPrefix(stem, sig+"-")
		if !ok {
			continue
		}
		if rotatedTimestamp.MatchString(ts) {
			return sig
		}
	}
	return ""
}

func stripPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return s, false
}

func stripSuffix(s, suffix string) (string, bool) {
	if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)], true
	}
	return s, false
}

func signalOrder(sig string) int {
	for i, s := range signals {
		if s == sig {
			return i
		}
	}
	return len(signals)
}

// IsValidSignal reports whether sig names a known telemetry signal. Used by
// the DELETE handler to reject invalid query parameters before touching disk.
func IsValidSignal(sig string) bool {
	for _, s := range signals {
		if s == sig {
			return true
		}
	}
	return false
}
