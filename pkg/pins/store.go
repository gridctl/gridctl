package pins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/state"
)

const (
	lockTimeout = 5 * time.Second
	fileVersion = "1"
	filePerm    = os.FileMode(0600)
)

// PinStore manages TOFU schema pins for a deployed stack.
// It is safe for concurrent use: in-memory access is guarded by a RWMutex,
// and disk writes are serialized via state.WithLock.
type PinStore struct {
	stackName string
	path      string
	mu        sync.RWMutex
	data      *PinFile
}

// New creates a PinStore for the given stack name.
// The pin file lives at ~/.gridctl/pins/{stackName}.json.
// Call Load() before performing verification or pinning operations.
func New(stackName string) *PinStore {
	ps := &PinStore{
		stackName: stackName,
		path:      pinsPath(stackName),
	}
	ps.data = ps.emptyPinFile()
	return ps
}

// pinsPath returns the path to the pin file for a stack.
// Phase 2 adds state.PinsPath() — this helper bridges the gap.
func pinsPath(stackName string) string {
	return filepath.Join(state.BaseDir(), "pins", stackName+".json")
}

// Load reads the pin file from disk into memory.
// If the file does not exist, the store starts empty (ready for first pin).
// If the file is corrupt, it is discarded and a warning is logged.
func (ps *PinStore) Load() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	data, err := os.ReadFile(ps.path)
	if err != nil {
		if os.IsNotExist(err) {
			ps.data = ps.emptyPinFile()
			return nil
		}
		return fmt.Errorf("pins: reading pin file: %w", err)
	}

	var pf PinFile
	if err := json.Unmarshal(data, &pf); err != nil {
		slog.Warn("pins: corrupt pin file, starting fresh", "path", ps.path, "error", err)
		ps.data = ps.emptyPinFile()
		return nil
	}
	if pf.Servers == nil {
		pf.Servers = make(map[string]*ServerPins)
	}
	ps.data = &pf
	return nil
}

// GetAll returns a snapshot of all server pin records.
func (ps *PinStore) GetAll() map[string]*ServerPins {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	out := make(map[string]*ServerPins, len(ps.data.Servers))
	for k, v := range ps.data.Servers {
		out[k] = v
	}
	return out
}

// GetServer returns the pin record for a single server.
func (ps *PinStore) GetServer(serverName string) (*ServerPins, bool) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	sp, ok := ps.data.Servers[serverName]
	return sp, ok
}

// VerifyOrPin is the primary entry point called on RefreshTools.
// On first use it pins the tools; on subsequent calls it verifies against pins.
// New tools not in pins are auto-pinned; modified tools trigger VerifyStatusDrift.
func (ps *PinStore) VerifyOrPin(serverName string, tools []mcp.Tool) (*VerifyResult, error) {
	return ps.withFileLock(func() (*VerifyResult, error) {
		ps.mu.Lock()
		defer ps.mu.Unlock()

		sp := ps.data.Servers[serverName]
		if sp == nil {
			// First time — pin everything.
			result, err := ps.pinServer(serverName, tools)
			if err != nil {
				return nil, err
			}
			if err := ps.saveLocked(); err != nil {
				return nil, err
			}
			return result, nil
		}

		return ps.verifyAndUpdate(serverName, sp, tools)
	})
}

// Verify checks tools against stored pins without pinning new tools.
// Unlike VerifyOrPin, new tools are not auto-pinned.
func (ps *PinStore) Verify(serverName string, tools []mcp.Tool) (*VerifyResult, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	sp := ps.data.Servers[serverName]
	if sp == nil {
		return &VerifyResult{ServerName: serverName, Status: VerifyStatusPinned}, nil
	}

	return ps.buildVerifyResult(serverName, sp, tools)
}

// Approve re-pins the current tool definitions for a server, clearing drift.
func (ps *PinStore) Approve(serverName string, tools []mcp.Tool) error {
	_, err := ps.withFileLock(func() (*VerifyResult, error) {
		ps.mu.Lock()
		defer ps.mu.Unlock()

		if _, err := ps.pinServer(serverName, tools); err != nil {
			return nil, err
		}
		return nil, ps.saveLocked()
	})
	return err
}

// Reset deletes the pin record for a server. The next VerifyOrPin call will re-pin.
func (ps *PinStore) Reset(serverName string) error {
	_, err := ps.withFileLock(func() (*VerifyResult, error) {
		ps.mu.Lock()
		defer ps.mu.Unlock()

		delete(ps.data.Servers, serverName)
		return nil, ps.saveLocked()
	})
	return err
}

// --- internal helpers ---

// withFileLock runs fn under a file-level lock to serialize writes across processes.
func (ps *PinStore) withFileLock(fn func() (*VerifyResult, error)) (*VerifyResult, error) {
	var result *VerifyResult
	err := state.WithLock("pins-"+ps.stackName, lockTimeout, func() error {
		var e error
		result, e = fn()
		return e
	})
	return result, err
}

// pinServer hashes all provided tools and stores them as a fresh pin record.
// Caller must hold ps.mu.Lock().
func (ps *PinStore) pinServer(serverName string, tools []mcp.Tool) (*VerifyResult, error) {
	now := time.Now().UTC()
	toolRecords := make(map[string]*PinRecord, len(tools))
	hashes := make([]string, 0, len(tools))

	for _, t := range sortedTools(tools) {
		h, err := hashTool(t)
		if err != nil {
			return nil, fmt.Errorf("pins: hashing tool %q: %w", t.Name, err)
		}
		toolRecords[t.Name] = &PinRecord{
			Hash:        h,
			Name:        t.Name,
			Description: t.Description,
			PinnedAt:    now,
		}
		hashes = append(hashes, h)
	}

	// Preserve original PinnedAt when re-pinning (approve flow).
	pinnedAt := now
	if existing := ps.data.Servers[serverName]; existing != nil {
		pinnedAt = existing.PinnedAt
	}

	ps.data.Servers[serverName] = &ServerPins{
		ServerHash:     hashStrings(hashes),
		PinnedAt:       pinnedAt,
		LastVerifiedAt: now,
		ToolCount:      len(tools),
		Status:         StatusPinned,
		Tools:          toolRecords,
	}

	slog.Info("pins: pinned server", "server", serverName, "tools", len(tools))
	return &VerifyResult{ServerName: serverName, Status: VerifyStatusPinned}, nil
}

// verifyAndUpdate compares current tools against stored pins, updates state, and saves.
// Caller must hold ps.mu.Lock().
func (ps *PinStore) verifyAndUpdate(serverName string, sp *ServerPins, tools []mcp.Tool) (*VerifyResult, error) {
	result, err := ps.buildVerifyResult(serverName, sp, tools)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	sp.LastVerifiedAt = now

	if len(result.ModifiedTools) > 0 {
		sp.Status = StatusDrift
		slog.Warn("pins: schema drift detected",
			"server", serverName,
			"modified", len(result.ModifiedTools))
	} else {
		sp.Status = StatusPinned
	}

	// Auto-pin new tools (additions are not considered drift).
	if len(result.NewTools) > 0 {
		slog.Info("pins: new tools detected, pinning",
			"server", serverName,
			"tools", result.NewTools)
		for _, t := range tools {
			if sp.Tools[t.Name] != nil {
				continue
			}
			h, err := hashTool(t)
			if err != nil {
				return nil, fmt.Errorf("pins: hashing new tool %q: %w", t.Name, err)
			}
			sp.Tools[t.Name] = &PinRecord{
				Hash:        h,
				Name:        t.Name,
				Description: t.Description,
				PinnedAt:    now,
			}
		}
		sp.ToolCount = len(sp.Tools)
	}

	if len(result.RemovedTools) > 0 {
		slog.Warn("pins: tools removed since pinning",
			"server", serverName,
			"removed", result.RemovedTools)
	}

	sp.ServerHash = serverHashFromPins(sp.Tools)

	if err := ps.saveLocked(); err != nil {
		return nil, err
	}

	return result, nil
}

// buildVerifyResult computes the diff between current tools and stored pins.
// It does not mutate state. Caller must hold ps.mu (read or write lock).
func (ps *PinStore) buildVerifyResult(serverName string, sp *ServerPins, tools []mcp.Tool) (*VerifyResult, error) {
	result := &VerifyResult{ServerName: serverName, Status: VerifyStatusVerified}

	// Index current tools by name.
	current := make(map[string]mcp.Tool, len(tools))
	for _, t := range tools {
		current[t.Name] = t
	}

	// Check each pinned tool against the current tool list.
	for name, pin := range sp.Tools {
		t, present := current[name]
		if !present {
			result.RemovedTools = append(result.RemovedTools, name)
			continue
		}
		h, err := hashTool(t)
		if err != nil {
			return nil, fmt.Errorf("pins: hashing tool %q during verify: %w", name, err)
		}
		if h != pin.Hash {
			result.ModifiedTools = append(result.ModifiedTools, ToolDiff{
				Name:           name,
				OldHash:        pin.Hash,
				NewHash:        h,
				OldDescription: pin.Description,
				NewDescription: t.Description,
			})
		}
	}

	// Detect tools present on server but not yet pinned.
	for name := range current {
		if sp.Tools[name] == nil {
			result.NewTools = append(result.NewTools, name)
		}
	}

	// Assign summary status (drift takes priority).
	switch {
	case len(result.ModifiedTools) > 0:
		result.Status = VerifyStatusDrift
	case len(result.NewTools) > 0:
		result.Status = VerifyStatusNewTools
	case len(result.RemovedTools) > 0:
		result.Status = VerifyStatusRemovedTools
	}

	sort.Strings(result.NewTools)
	sort.Strings(result.RemovedTools)
	sort.Slice(result.ModifiedTools, func(i, j int) bool {
		return result.ModifiedTools[i].Name < result.ModifiedTools[j].Name
	})

	return result, nil
}

// saveLocked writes the pin file atomically. Creates the directory on first write.
// Caller must hold ps.mu.Lock().
func (ps *PinStore) saveLocked() error {
	dir := filepath.Dir(ps.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("pins: creating pins directory: %w", err)
	}

	data, err := json.MarshalIndent(ps.data, "", "  ")
	if err != nil {
		return fmt.Errorf("pins: marshaling pin file: %w", err)
	}

	return atomicWrite(ps.path, data, filePerm)
}

// atomicWrite writes data to path via a temp file and rename for crash safety.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return fmt.Errorf("pins: writing temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		if removeErr := os.Remove(tmp); removeErr != nil {
			slog.Warn("pins: failed to remove temp file after rename error", "path", tmp, "error", removeErr)
		}
		return fmt.Errorf("pins: renaming temp file: %w", err)
	}
	return nil
}

// emptyPinFile returns a fresh PinFile for this stack.
func (ps *PinStore) emptyPinFile() *PinFile {
	return &PinFile{
		Version:   fileVersion,
		Stack:     ps.stackName,
		CreatedAt: time.Now().UTC(),
		Servers:   make(map[string]*ServerPins),
	}
}

// --- hash functions ---

// hashTool computes a deterministic SHA256 digest of a tool's definition.
// inputSchema is canonically serialized (recursively sorted object keys) before hashing
// to ensure identical schemas produce identical hashes regardless of key order.
func hashTool(t mcp.Tool) (string, error) {
	canonical, err := canonicalSchema(t.InputSchema)
	if err != nil {
		return "", fmt.Errorf("pins: canonicalizing schema for %q: %w", t.Name, err)
	}
	sum := sha256.Sum256([]byte(t.Name + "\n" + t.Description + "\n" + canonical))
	return hex.EncodeToString(sum[:]), nil
}

// canonicalSchema produces a deterministic JSON string from a json.RawMessage
// by unmarshaling, recursively sorting all object keys, and re-marshaling.
// Go's encoding/json guarantees alphabetical key ordering when marshaling map[string]any.
func canonicalSchema(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "{}", nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("pins: parsing inputSchema: %w", err)
	}
	out, err := json.Marshal(sortKeys(v))
	if err != nil {
		return "", fmt.Errorf("pins: marshaling canonical schema: %w", err)
	}
	return string(out), nil
}

// sortKeys recursively rebuilds any map[string]any with recursively sorted values.
// encoding/json marshals map[string]any with alphabetically sorted keys, so this
// ensures nested maps also have their values recursively normalized.
func sortKeys(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, v2 := range val {
			out[k] = sortKeys(v2)
		}
		return out
	case []any:
		for i, item := range val {
			val[i] = sortKeys(item)
		}
		return val
	default:
		return v
	}
}

// hashStrings computes a SHA256 over the concatenation of a sorted slice of hex digests.
func hashStrings(hashes []string) string {
	var b strings.Builder
	for _, h := range hashes {
		b.WriteString(h)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// serverHashFromPins recomputes the server hash from stored pin records (sorted by name).
func serverHashFromPins(pins map[string]*PinRecord) string {
	names := make([]string, 0, len(pins))
	for n := range pins {
		names = append(names, n)
	}
	sort.Strings(names)

	hashes := make([]string, 0, len(names))
	for _, n := range names {
		hashes = append(hashes, pins[n].Hash)
	}
	return hashStrings(hashes)
}

// sortedTools returns a copy of tools sorted by name for deterministic hashing order.
func sortedTools(tools []mcp.Tool) []mcp.Tool {
	out := make([]mcp.Tool, len(tools))
	copy(out, tools)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
