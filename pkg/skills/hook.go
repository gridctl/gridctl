package skills

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// HookDefinition is a parsed Claude Code hook definition (hooks/<name>/hook.json).
// Raw is the file exactly as read; imports write it verbatim. The parsed
// form exists for validation, scanning, and listing. gridctl never executes
// a hook.
type HookDefinition struct {
	Name        string
	Description string
	Dialect     string
	// Events is the hooks object keys, sorted.
	Events []string
	// Scripts is the unique handler command paths, sorted, relative to the
	// hook directory with the leading "./" removed.
	Scripts []string
	// Warnings are unrecognized handler keys that passed through.
	Warnings []string
	// Raw is the file exactly as read.
	Raw []byte
}

// hookNamePattern matches valid hook names: the same charset rule as pack
// and agent names.
var hookNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// hookEventPattern matches Claude Code event names.
var hookEventPattern = regexp.MustCompile(`^[A-Z][A-Za-z]+$`)

// knownHookFields are the only accepted top-level keys.
var knownHookFields = map[string]bool{
	"name":        true,
	"description": true,
	"dialect":     true,
	"hooks":       true,
}

// passthroughHandlerKeys are handler keys copied verbatim. type and command
// are required and validated; the rest are optional.
var passthroughHandlerKeys = map[string]bool{
	"args":          true,
	"timeout":       true,
	"if":            true,
	"statusMessage": true,
	"async":         true,
	"asyncRewake":   true,
	"onFailure":     true,
}

// refusedHandlerKeys are handler keys that must not appear. shell is refused
// because the projected handler always runs in exec form; a shell key would
// route the pinned script back through a shell.
var refusedHandlerKeys = map[string]bool{
	"env":            true,
	"headers":        true,
	"url":            true,
	"allowedEnvVars": true,
	"shell":          true,
}

const (
	hookDialectClaudeCode = "claude-code"
	// hookInlineCommand is the refusal for a handler command that is not a
	// script shipped in the hook directory.
	hookInlineCommand = "hook handler commands must reference a script shipped in the hook directory"
)

// ValidateHookName validates a hook name: non-empty, lowercase letters,
// digits, and hyphens only.
func ValidateHookName(name string) error {
	if name == "" {
		return fmt.Errorf("hook name is required")
	}
	if !hookNamePattern.MatchString(name) {
		return fmt.Errorf("hook name %q must be lowercase letters, digits, and hyphens", name)
	}
	return nil
}

// ParseHookJSON parses and validates hook.json. hookDir is the directory
// that contains the file; handler commands are resolved against it and must
// be regular executable files inside it. Warnings for unrecognized handler
// keys are returned on the definition. A validation failure is an error
// that names the failing rule.
//
// The "if" field is a permission-rule pattern the client matches against the
// tool call (for example "Bash(git *)"), not code the client evaluates.
// Confirmed against the Claude Code hooks reference. A string passes
// through; any other type is refused.
func ParseHookJSON(data []byte, hookDir string) (*HookDefinition, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("hook.json is empty")
	}
	var raw map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parsing hook.json: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("parsing hook.json: extra data after the object")
	}
	// A JSON array or scalar decodes into a nil map without an error when
	// the target is a map and the value is null. Reject anything that is
	// not an object.
	var probe any
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("parsing hook.json: %w", err)
	}
	if _, ok := probe.(map[string]any); !ok {
		return nil, fmt.Errorf("hook.json must be an object")
	}

	name := jsonString(raw["name"])
	for key := range raw {
		if !knownHookFields[key] {
			label := name
			if label == "" {
				label = "(unnamed)"
			}
			return nil, fmt.Errorf("hook %s: unknown field %q", label, key)
		}
	}
	if err := walkHookStrings(probe, func(s string) error {
		if strings.Contains(s, "${var:") || strings.Contains(s, "${secret:") || strings.Contains(s, "${vault:") {
			label := name
			if label == "" {
				label = "(unnamed)"
			}
			return fmt.Errorf("hook %s: variable and secret references are not allowed in a hook definition", label)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := ValidateHookName(name); err != nil {
		return nil, err
	}
	descRaw, hasDesc := raw["description"]
	if !hasDesc || !jsonIsString(descRaw) || strings.TrimSpace(jsonString(descRaw)) == "" {
		return nil, fmt.Errorf("hook %s: description is required", name)
	}
	description := strings.TrimSpace(jsonString(descRaw))
	dialect := jsonString(raw["dialect"])
	if dialect != hookDialectClaudeCode {
		return nil, fmt.Errorf("hook %s: dialect %q is not supported (only %q)", name, dialect, hookDialectClaudeCode)
	}
	hooksRaw, ok := raw["hooks"]
	if !ok {
		return nil, fmt.Errorf("hook %s: hooks is required", name)
	}
	var events map[string]json.RawMessage
	if err := json.Unmarshal(hooksRaw, &events); err != nil {
		return nil, fmt.Errorf("hook %s: hooks must be an object", name)
	}
	if events == nil {
		return nil, fmt.Errorf("hook %s: hooks must be an object", name)
	}

	def := &HookDefinition{
		Name:        name,
		Description: description,
		Dialect:     dialect,
		Raw:         append([]byte(nil), data...),
	}
	eventNames := make([]string, 0, len(events))
	for event := range events {
		eventNames = append(eventNames, event)
	}
	sort.Strings(eventNames)
	scripts := map[string]struct{}{}
	for _, event := range eventNames {
		if !hookEventPattern.MatchString(event) {
			return nil, fmt.Errorf("hook %s: event name %q must match %s", name, event, hookEventPattern.String())
		}
		var groups []json.RawMessage
		if err := json.Unmarshal(events[event], &groups); err != nil {
			return nil, fmt.Errorf("hook %s: event %q must be an array of matcher groups", name, event)
		}
		for i, groupRaw := range groups {
			var group map[string]json.RawMessage
			if err := json.Unmarshal(groupRaw, &group); err != nil {
				return nil, fmt.Errorf("hook %s: event %q group %d must be an object", name, event, i)
			}
			for key := range group {
				if key != "matcher" && key != "hooks" {
					return nil, fmt.Errorf("hook %s: unknown matcher group key %q", name, key)
				}
			}
			if matcher, ok := group["matcher"]; ok {
				if !jsonIsString(matcher) {
					return nil, fmt.Errorf("hook %s: matcher must be a string", name)
				}
			}
			handlersRaw, ok := group["hooks"]
			if !ok {
				return nil, fmt.Errorf("hook %s: matcher group is missing \"hooks\"", name)
			}
			var handlers []json.RawMessage
			if err := json.Unmarshal(handlersRaw, &handlers); err != nil {
				return nil, fmt.Errorf("hook %s: event %q group %d hooks must be an array", name, event, i)
			}
			for _, handlerRaw := range handlers {
				rel, warnings, err := validateHookHandler(name, handlerRaw, hookDir)
				if err != nil {
					return nil, err
				}
				def.Warnings = append(def.Warnings, warnings...)
				if rel != "" {
					scripts[rel] = struct{}{}
				}
			}
		}
	}
	def.Events = eventNames
	for rel := range scripts {
		def.Scripts = append(def.Scripts, rel)
	}
	sort.Strings(def.Scripts)
	return def, nil
}

func validateHookHandler(name string, raw json.RawMessage, hookDir string) (string, []string, error) {
	var handler map[string]json.RawMessage
	if err := json.Unmarshal(raw, &handler); err != nil {
		return "", nil, fmt.Errorf("hook %s: handler must be an object", name)
	}
	var warnings []string
	for key := range handler {
		if refusedHandlerKeys[key] {
			return "", nil, fmt.Errorf("hook %s: handler key %q is not allowed", name, key)
		}
		if key == "type" || key == "command" || passthroughHandlerKeys[key] {
			continue
		}
		warnings = append(warnings, fmt.Sprintf("hook %s: unrecognized handler key %q passed through", name, key))
	}
	sort.Strings(warnings)
	typeRaw, ok := handler["type"]
	if !ok || !jsonIsString(typeRaw) {
		return "", nil, fmt.Errorf("hook %s: handler type is required", name)
	}
	handlerType := jsonString(typeRaw)
	if handlerType != "command" {
		return "", nil, fmt.Errorf("hook %s: handler type %q is not supported", name, handlerType)
	}
	if _, ok := handler["command"]; !ok || !jsonIsString(handler["command"]) {
		return "", nil, fmt.Errorf("hook %s: handler command is required", name)
	}
	command := jsonString(handler["command"])
	if err := validateHookCommandShape(name, command); err != nil {
		return "", nil, err
	}
	if args, ok := handler["args"]; ok {
		if err := validateHookArgs(name, args); err != nil {
			return "", nil, err
		}
	}
	if cond, ok := handler["if"]; ok && !jsonIsString(cond) {
		return "", nil, fmt.Errorf("hook %s: handler key \"if\" must be a string", name)
	}
	rel, err := resolveHookCommand(name, command, hookDir)
	if err != nil {
		return "", nil, err
	}
	return rel, warnings, nil
}

func validateHookArgs(name string, raw json.RawMessage) error {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return fmt.Errorf("hook %s: handler key \"args\" must be an array of strings", name)
	}
	for _, item := range items {
		if !jsonIsString(item) {
			return fmt.Errorf("hook %s: handler key \"args\" must be an array of strings", name)
		}
	}
	return nil
}

// validateHookCommandShape rejects inline shell strings, absolute paths, and
// ".." segments before the file is resolved. The inline refusal uses the
// fixed text callers match on.
func validateHookCommandShape(name, command string) error {
	if command == "" {
		return fmt.Errorf("hook %s: handler command is required", name)
	}
	if filepath.IsAbs(command) || pathIsAbsSlash(command) {
		return fmt.Errorf("hook %s: handler command %q is absolute", name, command)
	}
	for _, seg := range strings.Split(command, "/") {
		if seg == ".." {
			return fmt.Errorf("hook %s: handler command %q must not contain \"..\"", name, command)
		}
	}
	if strings.Contains(command, `\`) || !strings.HasPrefix(command, "./") || strings.ContainsAny(command, " \t|&;$`<>") {
		return fmt.Errorf("%s", hookInlineCommand)
	}
	return nil
}

func pathIsAbsSlash(p string) bool {
	return strings.HasPrefix(p, "/")
}

// resolveHookCommand resolves a "./"-relative command to a regular
// executable file inside hookDir. Symlinks are refused without being
// followed.
func resolveHookCommand(name, command, hookDir string) (string, error) {
	if hookDir == "" {
		return "", fmt.Errorf("hook %s: handler command %q cannot be resolved", name, command)
	}
	rel := strings.TrimPrefix(command, "./")
	if rel == "" || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("%s", hookInlineCommand)
	}
	clean := pathCleanSlash(rel)
	if clean == "" || clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", fmt.Errorf("hook %s: handler command %q must not contain \"..\"", name, command)
	}
	abs := filepath.Join(hookDir, filepath.FromSlash(clean))
	if !withinDir(hookDir, abs) {
		return "", fmt.Errorf("hook %s: handler command %q is outside the hook directory", name, command)
	}
	info, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("hook %s: handler command %q does not exist", name, command)
		}
		return "", fmt.Errorf("hook %s: handler command %q: %w", name, command, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("hook %s: handler command %q is a symlink", name, command)
	}
	if info.IsDir() {
		return "", fmt.Errorf("hook %s: handler command %q is a directory", name, command)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("hook %s: handler command %q is not a regular file", name, command)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("hook %s: handler command %q is not executable", name, command)
	}
	return clean, nil
}

func pathCleanSlash(p string) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
}

func jsonIsString(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	return raw[0] == '"'
}

func jsonString(raw json.RawMessage) string {
	if !jsonIsString(raw) {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func walkHookStrings(v any, fn func(string) error) error {
	switch t := v.(type) {
	case string:
		return fn(t)
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := walkHookStrings(t[k], fn); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range t {
			if err := walkHookStrings(item, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// ScanHook checks hook.json and every regular text file in hookDir for
// dangerous patterns. Like an agent definition, any finding of any severity
// blocks: the client executes this content. Symlinks are not followed.
// hook.json is scanned from hookJSON when those bytes are provided, and
// the walk skips that name so a definition finding is recorded once.
func ScanHook(name string, hookJSON []byte, hookDir string) *ScanResult {
	result := &ScanResult{SkillName: name, Safe: true}
	if len(hookJSON) > 0 {
		scanText("hook.json", string(hookJSON), result)
	}
	if hookDir != "" {
		_ = filepath.WalkDir(hookDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d == nil {
				return nil
			}
			if d.IsDir() || d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
				return nil
			}
			rel, _ := filepath.Rel(hookDir, path)
			rel = filepath.ToSlash(rel)
			if rel == "hook.json" && len(hookJSON) > 0 {
				return nil
			}
			data, rerr := os.ReadFile(path) // #nosec G304 -- walking a caller-supplied hook directory; symlinks are not followed
			if rerr != nil || !isScannable(data) {
				return nil
			}
			scanText(rel, string(data), result)
			return nil
		})
	}
	result.Safe = len(result.Findings) == 0
	return result
}

// HooksRoot returns the canonical hook store directory under a registry
// directory.
func HooksRoot(registryDir string) string {
	return filepath.Join(registryDir, "hooks")
}

// HookDir returns the canonical directory for one imported hook. The
// definition itself lives at HookDir(...)/hook.json.
func HookDir(registryDir, name string) string {
	return filepath.Join(HooksRoot(registryDir), name)
}

// InstalledHook is one hook present in the canonical store.
type InstalledHook struct {
	Name       string
	Definition *HookDefinition
	// Dir is the hook's canonical directory (holds hook.json and its
	// .origin.json sidecar).
	Dir string
	// Err is set when the directory is present but hook.json is missing
	// or does not validate. List and remove still see the hook. Info
	// and parse-required callers use GetHook, which returns the error.
	Err string
}

// ListHooks returns the hooks in the canonical store, sorted by name.
// A missing store directory is the normal no-hooks state. A directory
// whose hook.json is missing or does not validate is returned with Err
// set so a hand edit cannot hide the hook from list or remove.
func ListHooks(registryDir string) ([]InstalledHook, error) {
	root := HooksRoot(registryDir)
	dirs, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading hook store: %w", err)
	}
	var hooks []InstalledHook
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		dir := filepath.Join(root, d.Name())
		entry := InstalledHook{Name: d.Name(), Dir: dir}
		data, rerr := os.ReadFile(filepath.Join(dir, "hook.json")) // #nosec G304 -- fixed name inside the managed store
		if rerr != nil {
			entry.Err = rerr.Error()
			hooks = append(hooks, entry)
			continue
		}
		def, perr := ParseHookJSON(data, dir)
		if perr != nil {
			entry.Err = perr.Error()
			hooks = append(hooks, entry)
			continue
		}
		entry.Definition = def
		hooks = append(hooks, entry)
	}
	sort.Slice(hooks, func(i, j int) bool { return hooks[i].Name < hooks[j].Name })
	return hooks, nil
}

// GetHook returns one hook from the canonical store.
func GetHook(registryDir, name string) (*InstalledHook, error) {
	if err := ValidateHookName(name); err != nil {
		return nil, err
	}
	dir := HookDir(registryDir, name)
	data, err := os.ReadFile(filepath.Join(dir, "hook.json")) // #nosec G304 -- name validated above, fixed file name
	if err != nil {
		return nil, fmt.Errorf("hook %q not found: %w", name, err)
	}
	def, err := ParseHookJSON(data, dir)
	if err != nil {
		return nil, fmt.Errorf("hook %q: %w", name, err)
	}
	return &InstalledHook{Name: name, Definition: def, Dir: dir}, nil
}

// DeleteHook removes one hook from the canonical store, including its
// origin sidecar.
func DeleteHook(registryDir, name string) error {
	if err := ValidateHookName(name); err != nil {
		return err
	}
	dir := HookDir(registryDir, name)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("hook %q not found", name)
		}
		return err
	}
	return os.RemoveAll(dir)
}

// HookImportHint is the skill add notice when a repository ships hook
// definitions. Hooks import only through a pack manifest.
func HookImportHint(n int) string {
	return fmt.Sprintf("%d hook definition(s) not imported; hooks import through a pack manifest (gridctl pack add)", n)
}

// hooksDiscoveredError wraps an import failure that still discovered hook
// definitions, so skill add can print the pack hint and then exit as it
// does today.
type hooksDiscoveredError struct {
	count int
	err   error
}

func (e *hooksDiscoveredError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *hooksDiscoveredError) Unwrap() error { return e.err }

// DiscoveredHookCount reports hook definitions discovered on an import that
// did not import them. Zero when err does not carry that count.
func DiscoveredHookCount(err error) int {
	var he *hooksDiscoveredError
	if errors.As(err, &he) {
		return he.count
	}
	return 0
}

// installHookTree copies a hook directory into dstDir. hook.json is written
// verbatim from raw. Other files follow the own-directory skill install
// rules: dot-prefixed entries, symlinks, irregular files, and nested skill
// or hook directories are skipped with a warning. Executable modes are
// preserved. The same per-file, file-count, and total-size caps as skills
// apply. Dot entries already in dstDir, including .origin.json and
// hook.json.pre-* backups, are left in place.
func installHookTree(srcDir, dstDir string, raw []byte) ([]string, error) {
	files, warnings, err := collectHookFiles(srcDir)
	if err != nil {
		return warnings, err
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return warnings, fmt.Errorf("creating hook directory: %w", err)
	}
	hookFile := filepath.Join(dstDir, "hook.json")
	if !withinDir(dstDir, hookFile) {
		return warnings, fmt.Errorf("refusing to write outside hook directory")
	}
	if err := atomicWriteBytes(hookFile, raw); err != nil {
		return warnings, fmt.Errorf("writing hook.json: %w", err)
	}
	keep := map[string]struct{}{"hook.json": {}}
	for _, f := range files {
		out := filepath.Join(dstDir, filepath.FromSlash(f.rel))
		if !withinDir(dstDir, out) {
			return warnings, fmt.Errorf("refusing to write outside hook directory: %s", f.rel)
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return warnings, fmt.Errorf("creating %s: %w", filepath.Dir(out), err)
		}
		mode := f.mode
		if mode == 0 {
			mode = 0o644
		}
		// WriteFile does not change the mode of a file that already
		// exists. A re-import that turns the executable bit on must
		// still leave a parseable script.
		if err := os.WriteFile(out, f.content, mode); err != nil {
			return warnings, fmt.Errorf("writing %s: %w", f.rel, err)
		}
		if err := os.Chmod(out, mode); err != nil {
			return warnings, fmt.Errorf("setting mode on %s: %w", f.rel, err)
		}
		keep[f.rel] = struct{}{}
	}
	if err := pruneHookTree(dstDir, keep); err != nil {
		return warnings, err
	}
	return warnings, nil
}

// collectHookFiles selects the files installed beside hook.json. hook.json
// itself is written separately so the fetched bytes stay verbatim.
func collectHookFiles(srcDir string) ([]supportingFile, []string, error) {
	var (
		files    []supportingFile
		warnings []string
		total    int64
	)
	add := func(rel, abs string, d fs.DirEntry) error {
		info, err := d.Info()
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: cannot stat: %v", rel, err))
			return nil
		}
		if info.Size() > maxSupportingFileSize {
			return &limitError{reason: fmt.Sprintf("%s is %d bytes, over the %d byte per-file limit", rel, info.Size(), maxSupportingFileSize)}
		}
		if len(files) >= maxSupportingFiles {
			return &limitError{reason: fmt.Sprintf("more than %d supporting files", maxSupportingFiles)}
		}
		if total+info.Size() > maxSupportingTotalSize {
			return &limitError{reason: fmt.Sprintf("supporting files exceed the %d byte total limit", maxSupportingTotalSize)}
		}
		data, err := os.ReadFile(abs) // #nosec G304 -- abs is produced by walking srcDir, and symlinks are skipped before this point
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: cannot read: %v", rel, err))
			return nil
		}
		total += int64(len(data))
		files = append(files, supportingFile{rel: rel, mode: info.Mode().Perm(), content: data})
		return nil
	}
	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == srcDir {
				return fmt.Errorf("reading %s: %w", srcDir, walkErr)
			}
			rel, _ := filepath.Rel(srcDir, path)
			warnings = append(warnings, fmt.Sprintf("%s: cannot walk: %v", filepath.ToSlash(rel), walkErr))
			return nil
		}
		rel, rerr := filepath.Rel(srcDir, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		base := d.Name()
		if rel == "hook.json" {
			return nil
		}
		if strings.HasPrefix(base, ".") {
			warnings = append(warnings, fmt.Sprintf("%s: skipped dot entry", rel))
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.Contains(rel, "/") && strings.HasPrefix(base, "hook.json.") {
			warnings = append(warnings, fmt.Sprintf("%s: skipped reserved name", rel))
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			warnings = append(warnings, fmt.Sprintf("%s: skipped symlink", rel))
			return nil
		}
		if d.IsDir() {
			if _, serr := os.Lstat(filepath.Join(path, "SKILL.md")); serr == nil {
				warnings = append(warnings, fmt.Sprintf("%s: skipped nested skill directory", rel))
				return fs.SkipDir
			}
			if _, serr := os.Lstat(filepath.Join(path, "hook.json")); serr == nil {
				warnings = append(warnings, fmt.Sprintf("%s: skipped nested hook directory", rel))
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			warnings = append(warnings, fmt.Sprintf("%s: skipped irregular file", rel))
			return nil
		}
		return add(rel, path, d)
	})
	if err != nil {
		return nil, warnings, err
	}
	return files, warnings, nil
}

// pruneHookTree removes installed files that are no longer in the source
// set. Dot entries, hook.json.pre-* backups, and nested skill or hook
// directories are spared.
func pruneHookTree(dstDir string, keep map[string]struct{}) error {
	return filepath.WalkDir(dstDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		rel, rerr := filepath.Rel(dstDir, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		base := d.Name()
		if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "hook.json.") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if _, serr := os.Lstat(filepath.Join(path, "SKILL.md")); serr == nil {
				return fs.SkipDir
			}
			if _, serr := os.Lstat(filepath.Join(path, "hook.json")); serr == nil {
				return fs.SkipDir
			}
			return nil
		}
		if _, ok := keep[rel]; ok {
			return nil
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing %s: %w", rel, err)
		}
		return nil
	})
}

// backupHookFile copies hook.json to hook.json.pre-<suffix> before a
// forced overwrite of locally edited bytes. A missing file is a no-op.
func backupHookFile(dir, suffix string) (string, error) {
	src := filepath.Join(dir, "hook.json")
	data, err := os.ReadFile(src) // #nosec G304 -- fixed name inside the registry hook dir
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading hook.json: %w", err)
	}
	if suffix == "" {
		suffix = "local"
	}
	name := "hook.json.pre-" + suffix
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return "", fmt.Errorf("writing hook backup: %w", err)
	}
	return name, nil
}
