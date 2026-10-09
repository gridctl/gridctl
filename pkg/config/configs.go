package config

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultConfigMode = "0444"
	maxConfigEntries  = 64
	maxConfigBytes    = 1 << 20
	maxConfigID       = 1<<31 - 1
)

var configModePattern = regexp.MustCompile(`^[0-7]{3,4}$`)

// ReadConfigEntry resolves mode, ownership, and bytes for one config entry.
// file entries are read here. The returned error does not include file contents;
// a read error is the wrapped OS error, whose text may include the path.
func ReadConfigEntry(ctx context.Context, cfg ConfigFile) (mode int64, uid, gid int, body []byte, err error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, 0, nil, err
	}
	mode, err = configMode(cfg.Mode)
	if err != nil {
		return 0, 0, 0, nil, err
	}
	uid, err = configID(cfg.UID, "uid")
	if err != nil {
		return 0, 0, 0, nil, err
	}
	gid, err = configID(cfg.GID, "gid")
	if err != nil {
		return 0, 0, 0, nil, err
	}
	if cfg.File != "" {
		body, err = readConfigFile(ctx, cfg.File)
		if err != nil {
			return 0, 0, 0, nil, err
		}
		return mode, uid, gid, body, nil
	}
	if len(cfg.Content) > maxConfigBytes {
		return 0, 0, 0, nil, fmt.Errorf("content exceeds 1 MiB")
	}
	return mode, uid, gid, []byte(cfg.Content), nil
}

func configMode(mode string) (int64, error) {
	if mode == "" {
		mode = defaultConfigMode
	}
	if !configModePattern.MatchString(mode) {
		return 0, fmt.Errorf("mode %q is not three or four octal digits", mode)
	}
	parsed, err := strconv.ParseInt(mode, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("mode %q is not three or four octal digits", mode)
	}
	return parsed, nil
}

func configID(id *int, name string) (int, error) {
	if id == nil {
		return 0, nil
	}
	if *id < 0 || *id > maxConfigID {
		return 0, fmt.Errorf("%s must be between 0 and %d", name, maxConfigID)
	}
	return *id, nil
}

func readConfigFile(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	if info.Size() > maxConfigBytes {
		return nil, fmt.Errorf("file exceeds 1 MiB")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(body) > maxConfigBytes {
		return nil, fmt.Errorf("file exceeds 1 MiB")
	}
	return body, nil
}

// ConfigsRevision returns the hex sha256 of the resolved config entries.
// An empty list returns an empty revision and no label. File entries are read.
// Entries are ordered by target so YAML order does not change the digest.
func ConfigsRevision(ctx context.Context, configs []ConfigFile) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(configs) == 0 {
		return "", nil
	}
	type hashed struct {
		target  string
		mode    int64
		uid     int
		gid     int
		content []byte
	}
	entries := make([]hashed, 0, len(configs))
	for i, cfg := range configs {
		mode, uid, gid, body, err := ReadConfigEntry(ctx, cfg)
		if err != nil {
			return "", fmt.Errorf("config %d: %w", i, err)
		}
		entries = append(entries, hashed{target: cfg.Target, mode: mode, uid: uid, gid: gid, content: body})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].target < entries[j].target })
	hash := sha256.New()
	for _, entry := range entries {
		if err := writeRevisionField(hash, []byte(entry.target)); err != nil {
			return "", err
		}
		var nums [24]byte
		binary.BigEndian.PutUint64(nums[0:8], uint64(entry.mode))
		binary.BigEndian.PutUint64(nums[8:16], uint64(entry.uid))
		binary.BigEndian.PutUint64(nums[16:24], uint64(entry.gid))
		if _, err := hash.Write(nums[:]); err != nil {
			return "", err
		}
		if err := writeRevisionField(hash, entry.content); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeRevisionField(hash hash.Hash, value []byte) error {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(value)))
	if _, err := hash.Write(n[:]); err != nil {
		return err
	}
	_, err := hash.Write(value)
	return err
}

// ReferencedConfigFiles returns absolute paths for file-backed config entries.
// Values that need variable expansion or a tilde are refused. Absolute paths
// are returned as written. Relative paths are joined to stackDir.
func ReferencedConfigFiles(stack *Stack, stackDir string) ([]string, error) {
	if stack == nil {
		return nil, nil
	}
	var out []string
	for i, srv := range stack.MCPServers {
		for j, cfg := range srv.Configs {
			if cfg.File == "" {
				continue
			}
			if expandRegex.MatchString(cfg.File) || strings.HasPrefix(cfg.File, "~") {
				return nil, fmt.Errorf("export: mcp-servers[%d].configs[%d].file cannot be anchored without expansion; use a path relative to the stack file", i, j)
			}
			file := cfg.File
			if !filepath.IsAbs(file) {
				file = filepath.Join(stackDir, file)
			}
			out = append(out, file)
		}
	}
	return out, nil
}

func configFilesEqual(a, b []ConfigFile) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func validateConfigs(server MCPServer, prefix string) ValidationErrors {
	if len(server.Configs) == 0 {
		return nil
	}
	var errs ValidationErrors
	if !server.IsContainerBased() {
		errs = append(errs, ValidationError{prefix + ".configs", "only valid for container-based servers"})
	}
	if server.Execution != nil && server.Execution.Mode == "hardened" {
		errs = append(errs, ValidationError{prefix + ".configs", "not supported with execution.mode: hardened; declare an engine-local volume under execution.mounts and seed it separately (see docs/execution.md)"})
	}
	if len(server.Configs) > maxConfigEntries {
		errs = append(errs, ValidationError{prefix + ".configs", fmt.Sprintf("must have at most %d entries", maxConfigEntries)})
	}
	seen := map[string]bool{}
	volumeTargets := volumeContainerPaths(server.Volumes)
	for j, cfg := range server.Configs {
		errs = append(errs, validateConfigEntry(cfg, fmt.Sprintf("%s.configs[%d]", prefix, j), seen, volumeTargets)...)
	}
	return errs
}

func validateConfigEntry(cfg ConfigFile, field string, seen, volumeTargets map[string]bool) ValidationErrors {
	var errs ValidationErrors
	switch {
	case cfg.Target == "":
		errs = append(errs, ValidationError{field + ".target", "is required"})
	case !path.IsAbs(cfg.Target):
		errs = append(errs, ValidationError{field + ".target", "must be absolute"})
	case path.Clean(cfg.Target) != cfg.Target:
		errs = append(errs, ValidationError{field + ".target", "must be clean and must not contain traversal"})
	case cfg.Target == "/":
		errs = append(errs, ValidationError{field + ".target", "must not be /"})
	case forbiddenConfigTarget(cfg.Target):
		errs = append(errs, ValidationError{field + ".target", "must not be under /proc, /sys, or /dev"})
	case seen[cfg.Target]:
		errs = append(errs, ValidationError{field + ".target", fmt.Sprintf("duplicate target %q", cfg.Target)})
	case volumeTargets[cfg.Target]:
		errs = append(errs, ValidationError{field + ".target", "conflicts with a volume container path"})
	default:
		seen[cfg.Target] = true
	}
	hasFile := cfg.File != ""
	hasContent := cfg.Content != ""
	if hasFile == hasContent {
		errs = append(errs, ValidationError{field, "requires exactly one of file or content"})
	}
	if cfg.Mode != "" && !configModePattern.MatchString(cfg.Mode) {
		errs = append(errs, ValidationError{field + ".mode", "must be three or four octal digits (the 0o prefix is not accepted)"})
	}
	if cfg.UID != nil && (*cfg.UID < 0 || *cfg.UID > maxConfigID) {
		errs = append(errs, ValidationError{field + ".uid", fmt.Sprintf("must be between 0 and %d", maxConfigID)})
	}
	if cfg.GID != nil && (*cfg.GID < 0 || *cfg.GID > maxConfigID) {
		errs = append(errs, ValidationError{field + ".gid", fmt.Sprintf("must be between 0 and %d", maxConfigID)})
	}
	if len(cfg.Content) > maxConfigBytes {
		errs = append(errs, ValidationError{field + ".content", "must be at most 1 MiB"})
	}
	return errs
}

func forbiddenConfigTarget(target string) bool {
	for _, prefix := range []string{"/proc", "/sys", "/dev"} {
		if target == prefix || strings.HasPrefix(target, prefix+"/") {
			return true
		}
	}
	return false
}

func volumeContainerPaths(volumes []string) map[string]bool {
	out := map[string]bool{}
	for _, volume := range volumes {
		if containerPath, ok := volumeContainerPath(volume); ok {
			out[containerPath] = true
		}
	}
	return out
}

func volumeContainerPath(volume string) (string, bool) {
	_, rest, ok := splitVolume(volume)
	if !ok || len(rest) < 2 || rest[0] != ':' {
		return "", false
	}
	containerPath := rest[1:]
	if i := strings.LastIndex(containerPath, ":"); i >= 0 {
		mode := containerPath[i+1:]
		if mode == "ro" || mode == "rw" {
			containerPath = containerPath[:i]
		}
	}
	if containerPath == "" {
		return "", false
	}
	return containerPath, true
}
