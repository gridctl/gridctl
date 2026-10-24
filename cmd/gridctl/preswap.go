package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/gridctl/gridctl/pkg/versions"
	"gopkg.in/yaml.v3"
)

// preswapMaxExtendsDepth matches config.maxExtendsDepth. The pre-swap reader
// cannot import that unexported constant. A parent-only floor is missed if
// the chain stops early, so the depths stay equal.
const preswapMaxExtendsDepth = 10

// preswapInput is the pure input of the rollback compatibility check.
// Pin path funcs resolve a stack name to the tool and skill pin files.
// A missing file is compatible. Paths are read as named; nothing is expanded.
type preswapInput struct {
	TargetTag   string
	Target      *versions.Schemas
	Own         *versions.Schemas
	StateDir    string
	ProjectLock string
	ImportLock  string
	ToolPin     func(name string) (string, error)
	SkillPin    func(name string) (string, error)
}

// preswapResult is one line per problem. Refusals exit 2 unless --force
// downgrades them. Warnings never block.
type preswapResult struct {
	Refusals []string
	Warnings []string
}

type daemonHead struct {
	SchemaVersion int    `json:"schema_version"`
	StackName     string `json:"stack_name"`
	StackFile     string `json:"stack_file"`
	PID           int    `json:"pid"`
}

type pinHead struct {
	Version string `json:"version"`
}

type lockVersionHead struct {
	Version int `yaml:"version"`
}

type importLockHead struct {
	Version int                         `yaml:"version"`
	Sources map[string]importLockSource `yaml:"sources"`
}

type importLockSource struct {
	Pack *importLockPack `yaml:"pack"`
}

type importLockPack struct {
	Name     string `yaml:"name"`
	Requires string `yaml:"requires"`
}

type stackFloorDoc struct {
	Extends  string           `yaml:"extends"`
	Name     string           `yaml:"name"`
	Requires *stackFloorNeeds `yaml:"requires"`
}

type stackFloorNeeds struct {
	Gridctl string `yaml:"gridctl"`
}

type foundFloor struct {
	File       string
	Constraint string
}

// checkPreswap reports compatibility problems for a rollback target.
// It reads state files directly (state.List skips files it cannot load)
// and never prints credential fields. A nil target advertises nothing:
// the caller proceeds after the unknown-schemas warning.
func checkPreswap(ctx context.Context, in preswapInput) (preswapResult, error) {
	var out preswapResult
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if in.Target == nil {
		out.Warnings = append(out.Warnings, unknownSchemasWarning(in.TargetTag))
		return out, nil
	}
	heads, warnings, err := readDaemonHeads(ctx, in.StateDir)
	if err != nil {
		return out, err
	}
	out.Warnings = append(out.Warnings, warnings...)
	for _, h := range heads {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		out.Refusals = append(out.Refusals, daemonStateRefusals(in, h)...)
		pinRefusals, pinWarnings := pinRefusalsFor(in, h)
		out.Refusals = append(out.Refusals, pinRefusals...)
		out.Warnings = append(out.Warnings, pinWarnings...)
		floorRefusals, floorWarnings := stackFloorRefusals(ctx, in, h)
		out.Refusals = append(out.Refusals, floorRefusals...)
		out.Warnings = append(out.Warnings, floorWarnings...)
	}
	lockRefusals, lockWarnings, err := lockRefusals(ctx, in)
	if err != nil {
		return out, err
	}
	out.Refusals = append(out.Refusals, lockRefusals...)
	out.Warnings = append(out.Warnings, lockWarnings...)
	return out, nil
}

func unknownSchemasWarning(tag string) string {
	return fmt.Sprintf("warning: compatibility unknown: %s was retained by a release that does not advertise schemas", tag)
}

func readDaemonHeads(ctx context.Context, dir string) ([]namedHead, []string, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if dir == "" {
		return nil, nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("reading state directory %s: %w", dir, err)
	}
	var heads []namedHead
	var warnings []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path) // #nosec G304 -- path is a *.json file in the daemon state directory
		if err != nil {
			return nil, nil, fmt.Errorf("reading state file %s: %w", path, err)
		}
		var head daemonHead
		if err := json.Unmarshal(data, &head); err != nil {
			warnings = append(warnings, fmt.Sprintf("warning: %s could not be parsed; daemon state compatibility was not checked", path))
			continue
		}
		if head.StackName == "" {
			head.StackName = strings.TrimSuffix(entry.Name(), ".json")
		}
		heads = append(heads, namedHead{Path: path, Head: head})
	}
	return heads, warnings, nil
}

type namedHead struct {
	Path string
	Head daemonHead
}

func daemonStateRefusals(in preswapInput, h namedHead) []string {
	fileVer := h.Head.SchemaVersion
	// One line per file. When the file is newer than both readers, name
	// the target: that is the version the rollback would install, and it
	// may be lower than this binary. The current-binary line is only for
	// the case the target would accept the file.
	if fileVer > in.Target.DaemonState {
		return []string{fmt.Sprintf("%s is schema version %d; %s reads version %d. gridctl destroy %s before rolling back.",
			h.Path, fileVer, in.TargetTag, in.Target.DaemonState, h.Head.StackName)}
	}
	if in.Own != nil && fileVer > in.Own.DaemonState {
		return []string{fmt.Sprintf("%s is schema version %d; this gridctl reads version %d. gridctl destroy %s before rolling back.",
			h.Path, fileVer, in.Own.DaemonState, h.Head.StackName)}
	}
	return nil
}

func pinRefusalsFor(in preswapInput, h namedHead) (refusals, warnings []string) {
	if !safeStackName(h.Head.StackName) {
		warnings = append(warnings, fmt.Sprintf("warning: stack name %q is not a single path element; pin files were not checked", h.Head.StackName))
		return refusals, warnings
	}
	if in.ToolPin != nil {
		path, err := in.ToolPin(h.Head.StackName)
		r, w := onePin(in, h.Head.StackName, "tool", path, err, in.Target.ToolPins)
		refusals = append(refusals, r...)
		warnings = append(warnings, w...)
	}
	if in.SkillPin != nil {
		path, err := in.SkillPin(h.Head.StackName)
		r, w := onePin(in, h.Head.StackName, "skill", path, err, in.Target.SkillPins)
		refusals = append(refusals, r...)
		warnings = append(warnings, w...)
	}
	return refusals, warnings
}

func onePin(in preswapInput, stack, kind, path string, pathErr error, advertised string) (refusals, warnings []string) {
	if pathErr != nil {
		return nil, []string{fmt.Sprintf("warning: could not resolve the %s pin file for %s: %v", kind, stack, pathErr)}
	}
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is the pin file for a stack named by a daemon state file
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []string{fmt.Sprintf("warning: %s could not be read; %s pin compatibility for %s was not checked", path, kind, stack)}
	}
	var head pinHead
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, []string{fmt.Sprintf("warning: %s could not be parsed; %s pin compatibility for %s is unknown", path, kind, stack)}
	}
	if advertised == "" {
		return nil, []string{fmt.Sprintf("warning: %s does not advertise a %s pin version; pin compatibility for %s is unknown", in.TargetTag, kind, stack)}
	}
	targetVer, err := strconv.Atoi(advertised)
	if err != nil {
		return nil, []string{fmt.Sprintf("warning: %s advertises %s pin version %q, which is not an integer; pin compatibility for %s is unknown", in.TargetTag, kind, advertised, stack)}
	}
	fileVer, unknown, warn := parsePinFileVersion(head.Version)
	if unknown {
		return nil, []string{fmt.Sprintf("warning: %s has pin version %q, which is not an integer; pin compatibility for %s is unknown", path, head.Version, stack)}
	}
	if warn != "" {
		warnings = append(warnings, warn)
	}
	if fileVer > targetVer {
		refusals = append(refusals, fmt.Sprintf("%s is version %d; %s reads version %d. Delete or move the pin file, or destroy the stack, before rolling back; the daemon for %s will not start otherwise.",
			path, fileVer, in.TargetTag, targetVer, stack))
	}
	return refusals, warnings
}

// parsePinFileVersion parses a pin file version. An empty string is 0,
// matching what the stores accept. An unparsable value is unknown, never zero.
func parsePinFileVersion(raw string) (int, bool, string) {
	if raw == "" {
		return 0, false, ""
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, true, ""
	}
	return n, false, ""
}

func safeStackName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`)
}

func stackFloorRefusals(ctx context.Context, in preswapInput, h namedHead) (refusals, warnings []string) {
	if err := ctx.Err(); err != nil {
		return nil, []string{fmt.Sprintf("warning: stack floor check for %s skipped: %v", h.Head.StackName, err)}
	}
	path := strings.TrimSpace(h.Head.StackFile)
	if path == "" {
		return nil, nil
	}
	floors, warns, err := readStackFloors(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, []string{fmt.Sprintf("warning: stack file %s for %s is missing; version floor was not checked", path, h.Head.StackName)}
		}
		return nil, []string{fmt.Sprintf("warning: stack file %s for %s could not be parsed; version floor was not checked", path, h.Head.StackName)}
	}
	warnings = append(warnings, warns...)
	stack := h.Head.StackName
	for _, floor := range floors {
		if strings.TrimSpace(floor.Constraint) == "" {
			continue
		}
		ok, err := targetSatisfies(in.TargetTag, floor.Constraint)
		if err != nil {
			refusals = append(refusals, fmt.Sprintf("stack %s (%s) requires gridctl %s, which does not parse; cannot confirm %s satisfies it.",
				stack, floor.File, floor.Constraint, in.TargetTag))
			continue
		}
		if !ok {
			refusals = append(refusals, fmt.Sprintf("stack %s (%s) requires gridctl %s; %s does not satisfy it.",
				stack, floor.File, floor.Constraint, in.TargetTag))
		}
	}
	return refusals, warnings
}

// readStackFloors decodes only extends, name, and requires, and follows
// extends relative to each file up to preswapMaxExtendsDepth. It does not
// expand variables, open the vault, or resolve secrets.
func readStackFloors(path string) ([]foundFloor, []string, error) {
	visited := map[string]bool{}
	var floors []foundFloor
	var warnings []string
	err := walkStackFloors(path, 0, visited, &floors, &warnings)
	return floors, warnings, err
}

func walkStackFloors(path string, depth int, visited map[string]bool, floors *[]foundFloor, warnings *[]string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if visited[abs] {
		*warnings = append(*warnings, fmt.Sprintf("warning: stack file %s extends a file already in the chain; later parent floors were not checked", path))
		return nil
	}
	visited[abs] = true
	data, err := os.ReadFile(abs) // #nosec G304 -- path is a stack file named by a daemon state file, or its extends parent
	if err != nil {
		return err
	}
	var doc stackFloorDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	if doc.Requires != nil && strings.TrimSpace(doc.Requires.Gridctl) != "" {
		*floors = append(*floors, foundFloor{File: abs, Constraint: strings.TrimSpace(doc.Requires.Gridctl)})
	}
	extends := strings.TrimSpace(doc.Extends)
	if extends == "" {
		return nil
	}
	if strings.HasPrefix(extends, "http://") || strings.HasPrefix(extends, "https://") {
		*warnings = append(*warnings, fmt.Sprintf("warning: stack file %s extends a URL; parent floors were not checked", abs))
		return nil
	}
	if depth >= preswapMaxExtendsDepth {
		*warnings = append(*warnings, fmt.Sprintf("warning: stack file %s exceeds the extends depth of %d; later parent floors were not checked", abs, preswapMaxExtendsDepth))
		return nil
	}
	parent := extends
	if !filepath.IsAbs(parent) {
		parent = filepath.Join(filepath.Dir(abs), parent)
	}
	if err := walkStackFloors(parent, depth+1, visited, floors, warnings); err != nil {
		if os.IsNotExist(err) {
			*warnings = append(*warnings, fmt.Sprintf("warning: stack file %s extends %s, which is missing; parent floors were not checked", abs, parent))
			return nil
		}
		*warnings = append(*warnings, fmt.Sprintf("warning: stack file %s extends %s, which could not be parsed; parent floors were not checked", abs, parent))
	}
	return nil
}

func lockRefusals(ctx context.Context, in preswapInput) (refusals, warnings []string, err error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if in.ProjectLock != "" {
		ver, missing, readErr := readYAMLVersion(in.ProjectLock)
		if readErr != nil {
			warnings = append(warnings, fmt.Sprintf("warning: %s could not be parsed; project lock compatibility was not checked", in.ProjectLock))
		} else if !missing && ver > in.Target.ProjectLock {
			refusals = append(refusals, fmt.Sprintf("%s is version %d; %s reads version %d. Restore the copy under ~/.gridctl/project-migration-backup/ first.",
				in.ProjectLock, ver, in.TargetTag, in.Target.ProjectLock))
		}
	}
	if in.ImportLock != "" {
		r, w := importLockProblems(in)
		refusals = append(refusals, r...)
		warnings = append(warnings, w...)
	}
	return refusals, warnings, nil
}

func readYAMLVersion(path string) (int, bool, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is the project or import lock under the gridctl home
	if err != nil {
		if os.IsNotExist(err) {
			return 0, true, nil
		}
		return 0, false, err
	}
	var head lockVersionHead
	if err := yaml.Unmarshal(data, &head); err != nil {
		return 0, false, err
	}
	return head.Version, false, nil
}

func importLockProblems(in preswapInput) (refusals, warnings []string) {
	data, err := os.ReadFile(in.ImportLock) // #nosec G304 -- path is the import lock under the gridctl home
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []string{fmt.Sprintf("warning: %s could not be read; import lock compatibility was not checked", in.ImportLock)}
	}
	var head importLockHead
	if err := yaml.Unmarshal(data, &head); err != nil {
		return nil, []string{fmt.Sprintf("warning: %s could not be parsed; import lock compatibility was not checked", in.ImportLock)}
	}
	if head.Version > in.Target.ImportLock {
		names := packNames(head)
		remediation := "gridctl pack remove <name>"
		if len(names) > 0 {
			var cmds []string
			for _, name := range names {
				cmds = append(cmds, "gridctl pack remove "+name)
			}
			remediation = strings.Join(cmds, "; ")
		}
		refusals = append(refusals, fmt.Sprintf("%s is version %d; %s reads version %d. remove packs with the current build before rolling back (%s).",
			in.ImportLock, head.Version, in.TargetTag, in.Target.ImportLock, remediation))
	}
	for _, src := range head.Sources {
		if src.Pack == nil || strings.TrimSpace(src.Pack.Requires) == "" {
			continue
		}
		name := src.Pack.Name
		if name == "" {
			name = "unnamed"
		}
		constraint := strings.TrimSpace(src.Pack.Requires)
		ok, err := targetSatisfies(in.TargetTag, constraint)
		if err != nil {
			refusals = append(refusals, fmt.Sprintf("pack %s requires gridctl %s, which does not parse; cannot confirm %s satisfies it.",
				name, constraint, in.TargetTag))
			continue
		}
		if !ok {
			refusals = append(refusals, fmt.Sprintf("pack %s requires gridctl %s; %s does not satisfy it.",
				name, constraint, in.TargetTag))
		}
	}
	return refusals, warnings
}

func packNames(head importLockHead) []string {
	var names []string
	seen := map[string]bool{}
	for _, src := range head.Sources {
		if src.Pack == nil || src.Pack.Name == "" || seen[src.Pack.Name] {
			continue
		}
		seen[src.Pack.Name] = true
		names = append(names, src.Pack.Name)
	}
	return names
}

// targetSatisfies reports whether tag satisfies constraint. Pre-release
// versions are included, matching requires.gridctl. An empty constraint
// is not a constraint.
func targetSatisfies(tag, constraint string) (bool, error) {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return true, nil
	}
	parsed, err := semver.NewConstraint(constraint)
	if err != nil {
		return false, err
	}
	parsed.IncludePrerelease = true
	version, err := semver.NewVersion(strings.TrimPrefix(strings.TrimSpace(tag), "v"))
	if err != nil {
		return false, err
	}
	return parsed.Check(version), nil
}

// rollbackRefused formats the exit 2 block. force downgrades each refusal
// to a warning and returns false (proceed).
func rollbackRefused(tag string, result preswapResult, force bool) (proceed bool, lines []string) {
	var linesOut []string
	linesOut = append(linesOut, result.Warnings...)
	if len(result.Refusals) == 0 {
		return true, linesOut
	}
	if force {
		for _, r := range result.Refusals {
			linesOut = append(linesOut, "warning: "+r)
		}
		return true, linesOut
	}
	linesOut = append(linesOut, fmt.Sprintf("Rollback to %s refused:", tag))
	for _, r := range result.Refusals {
		linesOut = append(linesOut, "  "+r)
	}
	linesOut = append(linesOut, "Pass --force to roll back anyway.")
	return false, linesOut
}
