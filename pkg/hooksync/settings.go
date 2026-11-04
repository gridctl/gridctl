package hooksync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tailscale/hujson"

	"github.com/gridctl/gridctl/pkg/project"
)

const (
	backupSuffix     = ".gridctl-backup-"
	backupTimeFormat = "20060102-150405"
)

// locatedGroup is one settings matcher group whose handlers point at a
// hook's script directory.
type locatedGroup struct {
	Event   string
	Index   int
	Value   map[string]any
	Partial bool
}

type settingsFile struct {
	Path     string
	Raw      []byte
	Missing  bool
	Indent   string
	Tree     hujson.Value
	Parsed   bool
	ParseErr error
}

func readSettings(path string) (*settingsFile, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- settings path is the target table expanded against the manager home
	if err != nil {
		if os.IsNotExist(err) {
			return &settingsFile{Path: path, Missing: true, Indent: "  "}, nil
		}
		return nil, err
	}
	sf := &settingsFile{Path: path, Raw: data, Indent: detectIndent(data)}
	tree, perr := hujson.Parse(data)
	if perr != nil {
		sf.ParseErr = perr
		return sf, nil
	}
	sf.Tree = tree
	sf.Parsed = true
	return sf, nil
}

func detectIndent(raw []byte) string {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\n' || i+1 >= len(raw) {
			continue
		}
		j := i + 1
		for j < len(raw) && (raw[j] == ' ' || raw[j] == '\t') {
			j++
		}
		if j > i+1 && j < len(raw) && raw[j] != '\n' && raw[j] != '\r' {
			return string(raw[i+1 : j])
		}
	}
	return "  "
}

// locateGroups walks hooks.<Event> and returns groups that point at
// scriptDir. A group with one handler under the directory and one
// elsewhere is partial: a hand edit, not a foreign group.
func locateGroups(sf *settingsFile, scriptDir string) []locatedGroup {
	if sf == nil || !sf.Parsed {
		return nil
	}
	hooks := sf.Tree.Find("/hooks")
	if hooks == nil {
		return nil
	}
	events := objectKeys(hooks)
	sort.Strings(events)
	var located []locatedGroup
	for _, event := range events {
		for index := 0; ; index++ {
			el := sf.Tree.Find(fmt.Sprintf("/hooks/%s/%d", pointerEscape(event), index))
			if el == nil {
				break
			}
			value, err := decodeElement(el)
			if err != nil {
				continue
			}
			owned, partial := classifyGroup(value, scriptDir)
			if !owned {
				continue
			}
			located = append(located, locatedGroup{Event: event, Index: index, Value: value, Partial: partial})
		}
	}
	return located
}

func objectKeys(v *hujson.Value) []string {
	if v == nil {
		return nil
	}
	clone := v.Clone()
	clone.Standardize()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(clone.Pack(), &obj); err != nil {
		return nil
	}
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	return keys
}

func decodeElement(el *hujson.Value) (map[string]any, error) {
	clone := el.Clone()
	clone.Standardize()
	dec := json.NewDecoder(bytes.NewReader(clone.Pack()))
	dec.UseNumber()
	var value map[string]any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func classifyGroup(group map[string]any, scriptDir string) (owned, partial bool) {
	handlers, ok := group["hooks"].([]any)
	if !ok || len(handlers) == 0 {
		return false, false
	}
	under, other := 0, 0
	for _, item := range handlers {
		handler, ok := item.(map[string]any)
		if !ok {
			other++
			continue
		}
		typ, _ := handler["type"].(string)
		command, _ := handler["command"].(string)
		if typ == "command" && underScriptDir(command, scriptDir) {
			under++
			continue
		}
		other++
	}
	if under == 0 {
		return false, false
	}
	return true, other > 0
}

func underScriptDir(command, scriptDir string) bool {
	if command == "" || scriptDir == "" || !filepath.IsAbs(command) {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(scriptDir), filepath.Clean(command))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

func pointerEscape(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	return strings.ReplaceAll(s, "/", "~1")
}

type patchOp struct {
	Op    string
	Path  string
	Value []byte
}

func (op patchOp) render() string {
	if op.Value == nil {
		return fmt.Sprintf(`{"op":%q,"path":%q}`, op.Op, op.Path)
	}
	return fmt.Sprintf(`{"op":%q,"path":%q,"value":%s}`, op.Op, op.Path, op.Value)
}

func applyPatch(tree hujson.Value, ops []patchOp) ([]byte, error) {
	if len(ops) == 0 {
		return tree.Pack(), nil
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, op := range ops {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(op.render())
	}
	b.WriteByte(']')
	clone := tree.Clone()
	if err := clone.Patch([]byte(b.String())); err != nil {
		return nil, fmt.Errorf("patching settings: %w", err)
	}
	// Pack preserves the original trailing newline. Do not add one: that
	// would change a byte outside the patch.
	return clone.Pack(), nil
}

func prettyValue(v any, indent string) ([]byte, error) {
	return prettyPrefixed(v, "", indent)
}

func prettyPrefixed(v any, prefix, indent string) ([]byte, error) {
	if indent == "" {
		indent = "  "
	}
	return json.MarshalIndent(v, prefix, indent)
}

// appendOps builds add operations for groups that are not already in the
// file. A missing hooks key or event array is one pretty-printed value,
// not an empty container plus a later append. hujson drops leading
// whitespace on an inserted value, so the caller separates a glued key.
func appendOps(sf *settingsFile, groups []renderedGroup) ([]patchOp, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	indent := "  "
	if sf != nil {
		indent = sf.indentOrDefault()
	}
	hooksPresent := sf != nil && sf.Parsed && sf.Tree.Find("/hooks") != nil
	if !hooksPresent {
		value, err := prettyPrefixed(groupedHooks(groups), indent, indent)
		if err != nil {
			return nil, err
		}
		return []patchOp{{Op: "add", Path: "/hooks", Value: value}}, nil
	}
	byEvent := map[string][]renderedGroup{}
	var order []string
	for _, g := range groups {
		if _, ok := byEvent[g.Event]; !ok {
			order = append(order, g.Event)
		}
		byEvent[g.Event] = append(byEvent[g.Event], g)
	}
	var ops []patchOp
	var appending []renderedGroup
	for _, event := range order {
		ptr := "/hooks/" + pointerEscape(event)
		if sf.Tree.Find(ptr) == nil {
			arr := make([]any, 0, len(byEvent[event]))
			for _, g := range byEvent[event] {
				arr = append(arr, g.Value)
			}
			value, err := prettyPrefixed(arr, indent+indent, indent)
			if err != nil {
				return nil, err
			}
			ops = append(ops, patchOp{Op: "add", Path: ptr, Value: value})
			continue
		}
		appending = append(appending, byEvent[event]...)
	}
	for _, g := range appending {
		value, err := prettyInserted(g.Value, sf, g.Event)
		if err != nil {
			return nil, err
		}
		ops = append(ops, patchOp{Op: "add", Path: "/hooks/" + pointerEscape(g.Event) + "/-", Value: value})
	}
	return ops, nil
}

func groupedHooks(groups []renderedGroup) map[string][]any {
	out := map[string][]any{}
	for _, g := range groups {
		out[g.Event] = append(out[g.Event], g.Value)
	}
	return out
}

// separateNewKeys puts a key this patch inserted on its own line when
// hujson glues `,"key":` to the previous value. Only that inserted
// separator changes. A key that already existed in before is left alone,
// including its surrounding bytes.
func separateNewKeys(before, after []byte, indent string) []byte {
	if indent == "" {
		indent = "  "
	}
	out := after
	for _, key := range insertedKeys(before, after) {
		pad := indent
		if key != "hooks" {
			pad = indent + indent
		}
		out = separateKey(before, out, key, pad)
	}
	return out
}

func insertedKeys(before, after []byte) []string {
	var keys []string
	seen := map[string]bool{}
	rest := after
	for {
		idx := bytes.Index(rest, []byte(`,"`))
		if idx < 0 || idx+2 >= len(rest) {
			break
		}
		rest = rest[idx+2:]
		end := bytes.IndexByte(rest, '"')
		if end <= 0 {
			break
		}
		key := string(rest[:end])
		rest = rest[end:]
		if seen[key] || bytes.Contains(before, []byte(`"`+key+`"`)) {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys
}

func separateKey(before, after []byte, key, pad string) []byte {
	if bytes.Contains(before, []byte(`"`+key+`"`)) {
		return after
	}
	glued := []byte(`,"` + key + `":`)
	idx := bytes.Index(after, glued)
	if idx < 0 {
		return after
	}
	rest := after[idx+len(glued):]
	// hujson drops the value's leading whitespace, so "{" or "[" sits on
	// the colon. A space makes the inserted object read like the file.
	if len(rest) > 0 && (rest[0] == '{' || rest[0] == '[') {
		rest = append([]byte{' '}, rest...)
	}
	replacement := append([]byte(",\n"+pad+`"`+key+`":`), rest...)
	return append(append([]byte{}, after[:idx]...), replacement...)
}

// prettyInserted renders a group with the file's indent and a leading
// newline so an append reads like the surrounding array. The newline is
// part of the patch value's leading whitespace, which hujson preserves.
func prettyInserted(v any, sf *settingsFile, event string) ([]byte, error) {
	unit := sf.indentOrDefault()
	base := arrayElementIndent(sf.Raw, event, unit)
	data, err := json.MarshalIndent(v, base, unit)
	if err != nil {
		return nil, err
	}
	if len(sf.Raw) == 0 {
		return data, nil
	}
	return append([]byte("\n"), data...), nil
}

func arrayElementIndent(raw []byte, event, unit string) string {
	fallback := strings.Repeat(unit, 3)
	if len(raw) == 0 {
		return fallback
	}
	idx := bytes.Index(raw, []byte(`"`+event+`"`))
	if idx < 0 {
		return fallback
	}
	rest := raw[idx:]
	bracket := bytes.IndexByte(rest, '[')
	if bracket < 0 {
		return fallback
	}
	rest = rest[bracket+1:]
	for i := 0; i < len(rest); i++ {
		if rest[i] != '\n' {
			continue
		}
		j := i + 1
		for j < len(rest) && (rest[j] == ' ' || rest[j] == '\t') {
			j++
		}
		if j > i+1 && j < len(rest) && rest[j] != '\n' && rest[j] != ']' && rest[j] != '}' {
			return string(rest[i+1 : j])
		}
	}
	return fallback
}

// replaceOps rewrites located groups in place when the planned set has
// the same count per event. Otherwise the caller removes and appends.
func replaceOps(located []locatedGroup, groups []renderedGroup, indent string) ([]patchOp, error) {
	byEvent := map[string][]locatedGroup{}
	for _, g := range located {
		byEvent[g.Event] = append(byEvent[g.Event], g)
	}
	planned := map[string][]renderedGroup{}
	for _, g := range groups {
		planned[g.Event] = append(planned[g.Event], g)
	}
	if len(byEvent) != len(planned) {
		return nil, fmt.Errorf("event set changed")
	}
	var ops []patchOp
	for event, got := range byEvent {
		want := planned[event]
		if len(got) != len(want) {
			return nil, fmt.Errorf("group count changed")
		}
		sort.Slice(got, func(i, j int) bool { return got[i].Index < got[j].Index })
		for i, loc := range got {
			value, err := prettyValue(want[i].Value, indent)
			if err != nil {
				return nil, err
			}
			ops = append(ops, patchOp{
				Op:    "replace",
				Path:  fmt.Sprintf("/hooks/%s/%d", pointerEscape(event), loc.Index),
				Value: value,
			})
		}
	}
	return ops, nil
}

func removeOps(located []locatedGroup) []patchOp {
	sorted := append([]locatedGroup(nil), located...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Event != sorted[j].Event {
			return sorted[i].Event > sorted[j].Event
		}
		return sorted[i].Index > sorted[j].Index
	})
	var ops []patchOp
	for _, g := range sorted {
		ops = append(ops, patchOp{Op: "remove", Path: fmt.Sprintf("/hooks/%s/%d", pointerEscape(g.Event), g.Index)})
	}
	return ops
}

// dropEmptyEvents removes event arrays that the preceding removes emptied.
// lengths is the element count before those removes.
func dropEmptyEvents(lengths map[string]int, removed []locatedGroup) []patchOp {
	count := map[string]int{}
	for _, g := range removed {
		count[g.Event]++
	}
	var events []string
	for event, n := range count {
		if lengths[event] == n && n > 0 {
			events = append(events, event)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(events)))
	var ops []patchOp
	for _, event := range events {
		ops = append(ops, patchOp{Op: "remove", Path: "/hooks/" + pointerEscape(event)})
	}
	return ops
}

func (sf *settingsFile) indentOrDefault() string {
	if sf == nil || sf.Indent == "" {
		return "  "
	}
	return sf.Indent
}

func eventLengths(sf *settingsFile) map[string]int {
	out := map[string]int{}
	if sf == nil || !sf.Parsed {
		return out
	}
	for _, event := range objectKeys(sf.Tree.Find("/hooks")) {
		n := 0
		for sf.Tree.Find(fmt.Sprintf("/hooks/%s/%d", pointerEscape(event), n)) != nil {
			n++
		}
		out[event] = n
	}
	return out
}

func writeSettings(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := existingSettingsMode(path)
	if err := project.AtomicWriteFile(path, data); err != nil {
		return err
	}
	if mode == 0 {
		return nil
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("restoring settings mode: %w", err)
	}
	return nil
}

// existingSettingsMode is the source mode clamped so a rewrite never
// grants group or other write. Zero means the file is new.
func existingSettingsMode(path string) os.FileMode {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return 0
	}
	return clampSettingsMode(info.Mode())
}

// clampSettingsMode keeps owner read/write and group/other read, and
// drops anything wider than 0644. A mode with no remaining bits becomes
// 0600 rather than world-readable.
func clampSettingsMode(mode os.FileMode) os.FileMode {
	perm := mode.Perm() & 0o644
	if perm == 0 {
		return 0o600
	}
	return perm
}

func newSettings(groups []renderedGroup, indent string) ([]byte, error) {
	hooks := map[string]any{}
	var events []string
	seen := map[string]bool{}
	for _, g := range groups {
		if !seen[g.Event] {
			seen[g.Event] = true
			events = append(events, g.Event)
		}
	}
	sort.Strings(events)
	for _, event := range events {
		var arr []any
		for _, g := range groups {
			if g.Event == event {
				arr = append(arr, g.Value)
			}
		}
		hooks[event] = arr
	}
	if indent == "" {
		indent = "  "
	}
	data, err := json.MarshalIndent(map[string]any{"hooks": hooks}, "", indent)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func createBackup(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is the settings file just stated
	if err != nil {
		return "", fmt.Errorf("reading settings for backup: %w", err)
	}
	backupPath := path + backupSuffix + time.Now().UTC().Format(backupTimeFormat)
	for i := 0; ; i++ {
		candidate := backupPath
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", backupPath, i)
		}
		if _, statErr := os.Stat(candidate); os.IsNotExist(statErr) {
			backupPath = candidate
			break
		}
		if i == 20 {
			return "", fmt.Errorf("choosing a backup path for %s", path)
		}
	}
	if err := os.WriteFile(backupPath, data, clampSettingsMode(info.Mode())); err != nil {
		return "", fmt.Errorf("writing settings backup: %w", err)
	}
	pruneBackups(path)
	return backupPath, nil
}

func pruneBackups(originalPath string) {
	dir := filepath.Dir(originalPath)
	prefix := filepath.Base(originalPath) + backupSuffix
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var backups []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			backups = append(backups, filepath.Join(dir, entry.Name()))
		}
	}
	for _, p := range project.StaleBackups(backups, project.MaxBackups) {
		_ = os.Remove(p)
	}
}

func locatedAsRendered(located []locatedGroup) []renderedGroup {
	out := make([]renderedGroup, 0, len(located))
	for _, g := range located {
		if g.Partial {
			continue
		}
		out = append(out, renderedGroup{Event: g.Event, Value: g.Value})
	}
	return out
}
