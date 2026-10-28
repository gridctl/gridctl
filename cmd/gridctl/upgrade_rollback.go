package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/versions"
)

func (u *upgrader) rollbackTo(ctx context.Context, exePath string) error {
	if u.goos == "windows" {
		return fmt.Errorf("rollback is not supported on Windows")
	}
	dir, err := u.versionsDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return u.nothing("nothing retained; run gridctl upgrade first")
	}
	store, err := versions.Open(ctx, dir)
	if err != nil {
		return err
	}
	defer store.Close()

	target, err := u.rollbackTarget(store)
	if err != nil {
		return err
	}
	entry, path, ok := store.Lookup(target)
	if !ok {
		return u.notRetained(target)
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return u.notRetained(target)
		}
		return fmt.Errorf("retained %s: %w", target, err)
	}
	if err := store.Verify(ctx, target); err != nil {
		return err
	}
	if err := u.preSwap(ctx, target, entry.Schemas); err != nil {
		return err
	}

	currentTag := normalizeTag(u.version)
	fmt.Fprintf(u.stdout, "Current version: %s\n", currentTag)
	fmt.Fprintf(u.stdout, "Rollback target: %s\n", target)
	if err := u.confirm(
		fmt.Sprintf("\nRoll back gridctl to %s? [y/N] ", target),
		"non-interactive shell detected; pass --yes to confirm the rollback",
	); err != nil {
		if errors.Is(err, errUpgradeAborted) {
			return nil
		}
		return err
	}

	retainedCurrent := false
	if u.keep != 0 && retainableTag(currentTag) {
		store.AllowSources(exePath)
		if err := store.Retain(ctx, currentTag, exePath, versions.OriginReplaced, currentSchemas()); err != nil {
			return fmt.Errorf("retaining %s: %w", currentTag, err)
		}
		retainedCurrent = true
	}
	if err := atomicReplace(exePath, path); err != nil {
		return fmt.Errorf("replacing binary: %w", err)
	}
	if u.keep != 0 {
		previous := store.Previous()
		if retainedCurrent && currentTag != target {
			previous = currentTag
		}
		if err := store.SetActive(target, previous); err != nil {
			return err
		}
		if _, err := store.Prune(ctx, u.keep); err != nil {
			return fmt.Errorf("pruning retained binaries: %w", err)
		}
	}
	fmt.Fprintf(u.stdout, "\nRolled back gridctl from %s to %s\n", currentTag, target)
	return u.reportDaemons(ctx, exePath)
}

// preSwap runs the compatibility check before confirmation. Refusals exit 2
// unless --force, which prints them as warnings and continues.
func (u *upgrader) preSwap(ctx context.Context, target string, schemas *versions.Schemas) error {
	in, err := u.preswapInput(target, schemas)
	if err != nil {
		return err
	}
	result, err := checkPreswap(ctx, in)
	if err != nil {
		return err
	}
	proceed, lines := rollbackRefused(target, result, u.force)
	for _, line := range lines {
		fmt.Fprintln(u.stderr, line)
	}
	if !proceed {
		return &upgradeExit{Code: 2, msg: "rollback refused"}
	}
	return nil
}

func (u *upgrader) preswapInput(target string, schemas *versions.Schemas) (preswapInput, error) {
	in := preswapInput{
		TargetTag: target,
		Target:    schemas,
		Own:       currentSchemas(),
	}
	if schemas == nil {
		return in, nil
	}
	home, err := u.gridHome()
	if err != nil {
		return in, err
	}
	in.StateDir = filepath.Join(home, ".gridctl", "state")
	in.ProjectLock = filepath.Join(home, ".gridctl", "project.lock.yaml")
	in.ImportLock = filepath.Join(home, ".gridctl", "skills.lock.yaml")
	in.ToolPin = func(name string) (string, error) {
		return filepath.Join(home, ".gridctl", "pins", name+".json"), nil
	}
	in.SkillPin = func(name string) (string, error) {
		return filepath.Join(home, ".gridctl", "pins", "skills", name+".json"), nil
	}
	return in, nil
}

func (u *upgrader) notRetained(tag string) error {
	return u.nothing(fmt.Sprintf("%s is not retained (gridctl upgrade --list)", tag))
}

func (u *upgrader) rollbackTarget(store *versions.Store) (string, error) {
	if u.rollbackTag == "" || u.rollbackTag == rollbackPrevious {
		target := store.Previous()
		if target == "" {
			return "", u.nothing("nothing retained; run gridctl upgrade first")
		}
		return target, nil
	}
	target := normalizeTag(u.rollbackTag)
	if _, err := semver.NewVersion(strings.TrimPrefix(target, "v")); err != nil || versions.ValidateTag(target) != nil {
		return "", fmt.Errorf("rollback tag must be a semver version, got %q", u.rollbackTag)
	}
	if _, _, ok := store.Lookup(target); !ok {
		return "", u.notRetained(target)
	}
	return target, nil
}

func (u *upgrader) listRetained(ctx context.Context) error {
	dir, err := u.versionsDir()
	if err != nil {
		return err
	}
	man := versions.Manifest{SchemaVersion: versions.ManifestSchemaVersion, Entries: []versions.Entry{}}
	var orphans []string
	if _, err := os.Stat(dir); err == nil {
		store, err := versions.Open(ctx, dir)
		if err != nil {
			return err
		}
		defer store.Close()
		man.Active = store.Active()
		man.Previous = store.Previous()
		man.Entries = store.Entries()
		orphans = store.Orphans()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if man.Entries == nil {
		man.Entries = []versions.Entry{}
	}
	if orphans == nil {
		orphans = []string{}
	}
	if strings.EqualFold(u.format, "json") {
		// Additive machine-readable document. schema_version is required
		// so a later reader can refuse a newer shape (Article X).
		doc := listDocument{
			SchemaVersion: man.SchemaVersion,
			Active:        man.Active,
			Previous:      man.Previous,
			Entries:       man.Entries,
			Orphans:       orphans,
		}
		return output.EncodeJSON(u.stdout, doc)
	}
	writeListText(u.stdout, man, orphans)
	return nil
}

// listDocument is the --list --format json shape. orphans is computed, not
// stored: the manifest never deletes a directory it does not recognize.
type listDocument struct {
	SchemaVersion int              `json:"schema_version"`
	Active        string           `json:"active"`
	Previous      string           `json:"previous"`
	Entries       []versions.Entry `json:"entries"`
	Orphans       []string         `json:"orphans"`
}

func writeListText(w io.Writer, man versions.Manifest, orphans []string) {
	// Tag column is sized from the longest tag, minimum 10, plus one space
	// of gutter. Header and rows share that width. Origin stays 12 wide.
	width := 10
	for _, e := range man.Entries {
		if n := len(e.Tag) + 1; n > width {
			width = n
		}
	}
	fmt.Fprintf(w, "  %-*s%-12s%s\n", width, "TAG", "ORIGIN", "RETAINED              SIZE")
	for _, e := range man.Entries {
		mark := " "
		switch e.Tag {
		case man.Active:
			mark = "*"
		case man.Previous:
			mark = "p"
		}
		ts := e.RetainedAt.UTC().Format(time.RFC3339)
		fmt.Fprintf(w, "%s %-*s%-12s%s  %s\n", mark, width, e.Tag, e.Origin, ts, formatListSize(e.Size))
	}
	// Both counts are entries. --keep is per run and is not stored, so the
	// footer cannot report a keep limit. The specified example is N of N.
	fmt.Fprintf(w, "(* active, p previous; %d of %d kept)\n", len(man.Entries), len(man.Entries))
	if len(orphans) > 0 {
		fmt.Fprintf(w, "orphans (not deleted): %s\n", strings.Join(orphans, ", "))
	}
}

// formatListSize renders a retained binary size with one decimal place
// above bytes, matching the --list row (61.5 MiB). formatBytes rounds
// values of 10 or more to a whole number for the telemetry wipe summary
// and is the wrong formatter here.
func formatListSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	if exp >= len(units) {
		exp = len(units) - 1
	}
	return fmt.Sprintf("%.1f %s", float64(bytes)/float64(div), units[exp])
}
