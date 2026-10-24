package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/versions"
)

func TestUpgrade_ExistingFlowUnchanged(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	payload := []byte("new-binary-contents")
	srv, hits := releaseServer(t, "v1.2.3", payload)
	exe := writeExe(t, []byte("old-binary"))
	u, stdout, stderr := testUpgrader(t, srv, exe)
	u.version = "v1.0.0"

	if err := u.run(t.Context()); err != nil {
		t.Fatalf("upgrade: %v\nstderr: %s", err, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"Current version: v1.0.0",
		"Latest version: v1.2.3",
		"Downloading gridctl_1.2.3_",
		"Verifying SHA256",
		"✓ checksum matches",
		"Upgraded gridctl from v1.0.0 to v1.2.3",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("installed = %q", got)
	}
	if !hitPath(hits, "/repos/gridctl/gridctl/releases") {
		t.Fatalf("release API was not called: %v", hits.paths())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestUpgrade_RetainsReplacedAndInstalled(t *testing.T) {
	payload := []byte("installed-v1.2.3")
	srv, _ := releaseServer(t, "v1.2.3", payload)
	exe := writeExe(t, []byte("replaced-v1.0.0"))
	u, stdout, _ := testUpgrader(t, srv, exe)
	u.version = "v1.0.0"
	u.versionFlag = "v1.2.3"

	if err := u.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Retained v1.0.0 (roll back with: gridctl upgrade --rollback)") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	store := mustOpen(t, u.storeDir)
	defer store.Close()
	if store.Active() != "v1.2.3" || store.Previous() != "v1.0.0" {
		t.Fatalf("active=%s previous=%s", store.Active(), store.Previous())
	}
	if err := store.Verify(t.Context(), "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := store.Verify(t.Context(), "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	prev, _, ok := store.Lookup("v1.0.0")
	if !ok || prev.Origin != versions.OriginReplaced {
		t.Fatalf("replaced entry = %+v ok=%v", prev, ok)
	}
	wantSchemas := currentSchemas()
	if prev.Schemas == nil || *prev.Schemas != *wantSchemas {
		t.Fatalf("replaced schemas = %+v, want %+v", prev.Schemas, wantSchemas)
	}
	next, _, ok := store.Lookup("v1.2.3")
	if !ok || next.Origin != versions.OriginDownloaded || next.Schemas != nil {
		t.Fatalf("downloaded entry = %+v ok=%v", next, ok)
	}
}

func TestUpgrade_StoreHitSkipsDownload(t *testing.T) {
	payload := []byte("retained-bytes")
	srv, hits := releaseServer(t, "v1.2.3", payload)
	exe := writeExe(t, []byte("old"))
	u, _, _ := testUpgrader(t, srv, exe)
	u.version = "v1.0.0"
	u.versionFlag = "v1.2.3"
	if err := u.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	hits.reset()

	again, stdout, _ := testUpgrader(t, srv, exe)
	again.storeDir = u.storeDir
	again.version = "v1.2.3"
	again.versionFlag = "v1.2.3"
	if err := again.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Using retained v1.2.3 (no download)") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "Retained ") {
		t.Fatalf("same-tag reinstall reported a new retention:\n%s", stdout.String())
	}
	for _, path := range hits.paths() {
		if strings.Contains(path, "/releases/download/") {
			t.Fatalf("store hit downloaded %s", path)
		}
	}
	store := mustOpen(t, u.storeDir)
	defer store.Close()
	if store.Active() != "v1.2.3" || store.Previous() != "v1.0.0" {
		t.Fatalf("store hit rewrote rollback target: active=%s previous=%s", store.Active(), store.Previous())
	}
}

func TestUpgrade_SameTagStoreHitKeepsStoredBytes(t *testing.T) {
	payload := []byte("release-bytes")
	srv, hits := releaseServer(t, "v1.2.3", payload)
	exe := writeExe(t, []byte("old-binary"))
	u, _, _ := testUpgrader(t, srv, exe)
	u.version = "v1.0.0"
	u.versionFlag = "v1.2.3"
	if err := u.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	store := mustOpen(t, u.storeDir)
	entry, _, ok := store.Lookup("v1.2.3")
	store.Close()
	if !ok {
		t.Fatal("missing retained release")
	}
	wantSHA := entry.SHA256

	if err := os.WriteFile(exe, []byte("corrupted-local"), 0o755); err != nil {
		t.Fatal(err)
	}
	hits.reset()
	again, stdout, _ := testUpgrader(t, srv, exe)
	again.storeDir = u.storeDir
	again.version = "v1.2.3"
	again.versionFlag = "v1.2.3"
	if err := again.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Using retained v1.2.3 (no download)") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "Retained ") {
		t.Fatalf("same-tag reinstall reported a new retention:\n%s", stdout.String())
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("installed = %q, want stored release bytes", got)
	}
	store = mustOpen(t, u.storeDir)
	defer store.Close()
	entry, path, ok := store.Lookup("v1.2.3")
	if !ok || entry.SHA256 != wantSHA {
		t.Fatalf("store entry changed: %+v ok=%v", entry, ok)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, payload) {
		t.Fatalf("retained file = %q", stored)
	}
	if store.Previous() != "v1.0.0" || store.Active() != "v1.2.3" {
		t.Fatalf("active=%s previous=%s", store.Active(), store.Previous())
	}
	for _, hit := range hits.paths() {
		if strings.Contains(hit, "/releases/download/") {
			t.Fatalf("store hit downloaded %s", hit)
		}
	}
}

func TestUpgrade_KeepZeroLeavesStoreUntouched(t *testing.T) {
	first := []byte("first-binary")
	srv, _ := releaseServer(t, "v1.2.3", first)
	exe := writeExe(t, []byte("original"))
	u, _, _ := testUpgrader(t, srv, exe)
	u.version = "v1.0.0"
	u.versionFlag = "v1.2.3"
	if err := u.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(u.storeDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}

	second := []byte("second-binary")
	srv2, hits := releaseServer(t, "v1.3.0", second)
	next, stdout, _ := testUpgrader(t, srv2, exe)
	next.storeDir = u.storeDir
	next.version = "v1.2.3"
	next.versionFlag = "v1.3.0"
	next.keep = 0
	if err := next.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(u.storeDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("keep 0 rewrote the manifest:\n%s", after)
	}
	if _, err := os.Stat(filepath.Join(u.storeDir, "v1.3.0")); !os.IsNotExist(err) {
		t.Fatal("keep 0 retained the new tag")
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, second) {
		t.Fatalf("installed = %q", got)
	}
	if strings.Contains(stdout.String(), "Retained ") {
		t.Fatalf("keep 0 printed a retain line:\n%s", stdout.String())
	}
	if !hitPath(hits, "/releases/download/") {
		t.Fatal("keep 0 should still download when the tag is not the one being skipped")
	}
}

func TestUpgrade_KeepFloorWinsOverCount(t *testing.T) {
	dir := t.TempDir()
	exe := writeExe(t, []byte("start"))
	steps := []struct{ cur, next, body string }{
		{"v1.0.0", "v1.1.0", "body-11"},
		{"v1.1.0", "v1.2.0", "body-12"},
		{"v1.2.0", "v1.3.0", "body-13"},
	}
	for _, step := range steps {
		body := []byte(step.body)
		srv, _ := releaseServer(t, step.next, body)
		u, _, stderr := testUpgrader(t, srv, exe)
		u.storeDir = dir
		u.version = step.cur
		u.versionFlag = step.next
		u.keep = 1
		if err := u.run(t.Context()); err != nil {
			t.Fatalf("%s -> %s: %v\n%s", step.cur, step.next, err, stderr.String())
		}
	}
	store := mustOpen(t, dir)
	defer store.Close()
	if store.Active() != "v1.3.0" || store.Previous() != "v1.2.0" {
		t.Fatalf("active=%s previous=%s", store.Active(), store.Previous())
	}
	got := entryTags(store.Entries())
	if strings.Join(got, ",") != "v1.3.0,v1.2.0" {
		t.Fatalf("entries = %v, want active and previous despite --keep 1", got)
	}
}

func TestUpgrade_DevBuildNotRetained(t *testing.T) {
	payload := []byte("release")
	srv, _ := releaseServer(t, "v1.2.3", payload)
	exe := writeExe(t, []byte("dev-bytes"))
	u, stdout, _ := testUpgrader(t, srv, exe)
	u.version = "dev"
	u.versionFlag = "v1.2.3"
	if err := u.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "Retained ") {
		t.Fatalf("dev build was reported retained:\n%s", stdout.String())
	}
	store := mustOpen(t, u.storeDir)
	defer store.Close()
	if _, _, ok := store.Lookup("vdev"); ok {
		t.Fatal("dev tag was retained")
	}
	if _, _, ok := store.Lookup("v1.2.3"); !ok {
		t.Fatal("downloaded release should still be retained")
	}
	if store.Previous() != "" {
		t.Fatalf("previous = %q, want unchanged", store.Previous())
	}
}

func TestUpgrade_ListTextAndJSON(t *testing.T) {
	payload := []byte("listed")
	srv, _ := releaseServer(t, "v1.2.3", payload)
	exe := writeExe(t, []byte("old"))
	u, _, _ := testUpgrader(t, srv, exe)
	u.version = "v1.0.0"
	u.versionFlag = "v1.2.3"
	if err := u.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(u.storeDir, "v0.1.0"), 0o755); err != nil {
		t.Fatal(err)
	}

	list, stdout, _ := testUpgrader(t, srv, exe)
	list.storeDir = u.storeDir
	list.list = true
	if err := list.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	text := stdout.String()
	for _, want := range []string{"* v1.2.3", "p v1.0.0", "downloaded", "replaced", "(* active, p previous; 2 of 2 kept)", "orphans (not deleted): v0.1.0"} {
		if !strings.Contains(text, want) {
			t.Errorf("list text missing %q in:\n%s", want, text)
		}
	}

	empty, emptyOut, _ := testUpgrader(t, srv, exe)
	empty.list = true
	empty.format = "json"
	if err := empty.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	var doc listDocument
	if err := json.Unmarshal(emptyOut.Bytes(), &doc); err != nil {
		t.Fatalf("empty json: %v\n%s", err, emptyOut.String())
	}
	if doc.SchemaVersion != 1 || len(doc.Entries) != 0 || len(doc.Orphans) != 0 {
		t.Fatalf("empty doc = %+v", doc)
	}

	asJSON, jsonOut, _ := testUpgrader(t, srv, exe)
	asJSON.storeDir = u.storeDir
	asJSON.list = true
	asJSON.format = "json"
	if err := asJSON.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	doc = listDocument{}
	if err := json.Unmarshal(jsonOut.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.SchemaVersion != versions.ManifestSchemaVersion || doc.Active != "v1.2.3" || doc.Previous != "v1.0.0" {
		t.Fatalf("doc = %+v", doc)
	}
	if len(doc.Entries) != 2 || len(doc.Orphans) != 1 || doc.Orphans[0] != "v0.1.0" {
		t.Fatalf("doc entries=%d orphans=%v", len(doc.Entries), doc.Orphans)
	}
}

func TestUpgrade_RollbackPreviousAndNamedTag(t *testing.T) {
	dir := t.TempDir()
	exe := writeExe(t, []byte("start"))
	for _, step := range []struct{ cur, next, body string }{
		{"v1.0.0", "v1.1.0", "body-11"},
		{"v1.1.0", "v1.2.0", "body-12"},
	} {
		body := []byte(step.body)
		srv, _ := releaseServer(t, step.next, body)
		u, _, stderr := testUpgrader(t, srv, exe)
		u.storeDir = dir
		u.version = step.cur
		u.versionFlag = step.next
		u.keep = 5
		if err := u.run(t.Context()); err != nil {
			t.Fatalf("upgrade %s: %v\n%s", step.next, err, stderr.String())
		}
	}

	back, stdout, stderr := testUpgrader(t, nil, exe)
	back.storeDir = dir
	back.version = "v1.2.0"
	back.rollback = true
	back.keep = 5
	if err := back.run(t.Context()); err != nil {
		t.Fatalf("rollback: %v\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "warning: compatibility unknown: v1.1.0 was retained by a release that does not advertise schemas") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Rolled back gridctl from v1.2.0 to v1.1.0") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "body-11" {
		t.Fatalf("rolled back binary = %q", got)
	}
	store := mustOpen(t, dir)
	if store.Active() != "v1.1.0" || store.Previous() != "v1.2.0" {
		t.Fatalf("active=%s previous=%s", store.Active(), store.Previous())
	}
	store.Close()

	named, _, stderr := testUpgrader(t, nil, exe)
	named.storeDir = dir
	named.version = "v1.1.0"
	named.rollback = true
	named.rollbackTag = "v1.0.0"
	named.keep = 5
	if err := named.run(t.Context()); err != nil {
		t.Fatalf("named rollback: %v\n%s", err, stderr.String())
	}
	got, err = os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "start" {
		t.Fatalf("named rollback binary = %q", got)
	}
}

func TestUpgrade_RollbackExitCodes(t *testing.T) {
	exe := writeExe(t, []byte("installed"))
	u, _, stderr := testUpgrader(t, nil, exe)
	u.rollback = true
	err := u.run(t.Context())
	var code *upgradeExit
	if !errors.As(err, &code) || code.Code != 2 {
		t.Fatalf("empty rollback err = %v", err)
	}
	if !strings.Contains(stderr.String(), "nothing retained; run gridctl upgrade first") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	if got, _ := os.ReadFile(exe); string(got) != "installed" {
		t.Fatal("empty rollback replaced the binary")
	}

	dir := t.TempDir()
	seedRetained(t, dir, "v1.2.3", []byte("good"), true)
	missing, _, stderr := testUpgrader(t, nil, exe)
	missing.storeDir = dir
	missing.rollback = true
	missing.rollbackTag = "v0.9.0"
	err = missing.run(t.Context())
	code = nil
	if !errors.As(err, &code) || code.Code != 2 {
		t.Fatalf("absent tag err = %v", err)
	}
	if !strings.Contains(stderr.String(), "v0.9.0 is not retained (gridctl upgrade --list)") {
		t.Fatalf("stderr = %s", stderr.String())
	}

	if err := os.WriteFile(filepath.Join(dir, "v1.2.3", "gridctl"), []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	bad, _, stderr := testUpgrader(t, nil, exe)
	bad.storeDir = dir
	bad.rollback = true
	bad.rollbackTag = "v1.2.3"
	err = bad.run(t.Context())
	if err == nil || errors.As(err, &code) {
		t.Fatalf("checksum mismatch err = %v, want exit 1", err)
	}
	if !errors.Is(err, versions.ErrChecksumMismatch) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "expected:") || !strings.Contains(err.Error(), "actual:") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(stderr.String(), "expected:") || strings.Contains(stderr.String(), "actual:") {
		t.Fatalf("checksum mismatch also printed on stderr: %s", stderr.String())
	}
	if got, _ := os.ReadFile(exe); string(got) != "installed" {
		t.Fatal("checksum mismatch replaced the binary")
	}

	goneDir := t.TempDir()
	seedRetained(t, goneDir, "v1.0.0", []byte("gone-bytes"), true)
	if err := os.Remove(filepath.Join(goneDir, "v1.0.0", "gridctl")); err != nil {
		t.Fatal(err)
	}
	gone, _, stderr := testUpgrader(t, nil, exe)
	gone.storeDir = goneDir
	gone.rollback = true
	gone.rollbackTag = "v1.0.0"
	err = gone.run(t.Context())
	code = nil
	if !errors.As(err, &code) || code.Code != 2 {
		t.Fatalf("missing file err = %v", err)
	}
	if !strings.Contains(stderr.String(), "v1.0.0 is not retained (gridctl upgrade --list)") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	if strings.Contains(stderr.String(), "Error:") {
		t.Fatalf("missing file printed a second error line: %s", stderr.String())
	}
	if got, _ := os.ReadFile(exe); string(got) != "installed" {
		t.Fatal("missing file rollback replaced the binary")
	}

	untagged, _, stderr := testUpgrader(t, nil, exe)
	untagged.storeDir = goneDir
	untagged.rollback = true
	err = untagged.run(t.Context())
	code = nil
	if !errors.As(err, &code) || code.Code != 2 {
		t.Fatalf("tagless missing file err = %v", err)
	}
	if !strings.Contains(stderr.String(), "v1.0.0 is not retained (gridctl upgrade --list)") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestWriteListTextMatchesSpec(t *testing.T) {
	man := versions.Manifest{
		Active:   "v1.1.0",
		Previous: "v1.0.0",
		Entries: []versions.Entry{
			{
				Tag:        "v1.1.0",
				Origin:     versions.OriginDownloaded,
				RetainedAt: time.Date(2026, 10, 23, 14, 2, 12, 0, time.UTC),
				Size:       64487424, // 61.5 MiB
			},
			{
				Tag:        "v1.0.0",
				Origin:     versions.OriginReplaced,
				RetainedAt: time.Date(2026, 10, 23, 14, 2, 11, 0, time.UTC),
				Size:       61236838, // 58.4 MiB at one decimal
			},
		},
	}
	var buf bytes.Buffer
	writeListText(&buf, man, nil)
	want := "" +
		"  TAG       ORIGIN      RETAINED              SIZE\n" +
		"* v1.1.0    downloaded  2026-10-23T14:02:12Z  61.5 MiB\n" +
		"p v1.0.0    replaced    2026-10-23T14:02:11Z  58.4 MiB\n" +
		"(* active, p previous; 2 of 2 kept)\n"
	if buf.String() != want {
		t.Fatalf("list text =\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestUpgrade_RollbackPreswapRefuseAndForce(t *testing.T) {
	dir := t.TempDir()
	exe := writeExe(t, []byte("installed"))
	seedRetained(t, dir, "v1.0.0", []byte("old-bytes"), true)
	// Same-SHA retain is a no-op, so advertise schemas by rewriting the
	// manifest. A nil schemas entry would only warn and proceed.
	patchSchemas(t, dir, "v1.0.0", &versions.Schemas{DaemonState: 2, ProjectLock: 1, ImportLock: 1, ToolPins: "2", SkillPins: "1"})

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".gridctl"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".gridctl", "project.lock.yaml"), []byte("version: 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	refused, _, stderr := testUpgrader(t, nil, exe)
	refused.storeDir = dir
	refused.home = home
	refused.rollback = true
	refused.rollbackTag = "v1.0.0"
	refused.version = "v1.2.0"
	err := refused.run(t.Context())
	var code *upgradeExit
	if !errors.As(err, &code) || code.Code != 2 {
		t.Fatalf("refusal err = %v\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Rollback to v1.0.0 refused:") || !strings.Contains(stderr.String(), "project.lock.yaml is version 9") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Pass --force to roll back anyway.") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	if got, _ := os.ReadFile(exe); string(got) != "installed" {
		t.Fatal("refusal replaced the binary")
	}

	forced, _, stderr := testUpgrader(t, nil, exe)
	forced.storeDir = dir
	forced.home = home
	forced.rollback = true
	forced.rollbackTag = "v1.0.0"
	forced.version = "v1.2.0"
	forced.force = true
	if err := forced.run(t.Context()); err != nil {
		t.Fatalf("force rollback: %v\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "warning: ") || strings.Contains(stderr.String(), "Rollback to v1.0.0 refused:") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old-bytes" {
		t.Fatalf("forced binary = %q", got)
	}
}

func TestUpgrade_RollbackUsageAndHomebrewAndWindows(t *testing.T) {
	exe := writeExe(t, []byte("installed"))
	u, _, _ := testUpgrader(t, nil, exe)
	u.rollback = true
	u.rollbackTag = "../v1.0.0"
	if err := u.run(t.Context()); err == nil || errors.As(err, new(*upgradeExit)) {
		t.Fatalf("unsafe tag err = %v, want usage error", err)
	}

	brew, stdout, _ := testUpgrader(t, nil, exe)
	brew.resolveExe = func() (string, error) {
		return "/opt/homebrew/Cellar/gridctl/1.0.0/bin/gridctl", nil
	}
	brew.rollback = true
	if err := brew.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "installed via Homebrew") || !strings.Contains(stdout.String(), "brew upgrade gridctl/tap/gridctl") {
		t.Fatalf("stdout = %s", stdout.String())
	}

	win, _, _ := testUpgrader(t, nil, exe)
	win.goos = "windows"
	win.rollback = true
	err := win.run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "rollback is not supported on Windows") {
		t.Fatalf("windows rollback err = %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "installed" {
		t.Fatal("windows rollback replaced the binary")
	}
}

func TestUpgrade_WindowsSkipsRetention(t *testing.T) {
	payload := []byte("windows-release")
	srv, _ := releaseServer(t, "v1.2.3", payload)
	exe := writeExe(t, []byte("old"))
	u, _, stderr := testUpgrader(t, srv, exe)
	u.goos = "windows"
	u.version = "v1.0.0"
	u.versionFlag = "v1.2.3"
	if err := u.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if strings.Count(stderr.String(), "retention is not supported on Windows") != 1 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(u.storeDir, "manifest.json")); !os.IsNotExist(err) {
		t.Fatal("windows upgrade wrote the store")
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("installed = %q", got)
	}
}

func TestUpgrade_CheckAndUpToDateAndAbort(t *testing.T) {
	payload := []byte("newer")
	srv, hits := releaseServer(t, "v1.2.3", payload)
	exe := writeExe(t, []byte("current"))

	check, stdout, _ := testUpgrader(t, srv, exe)
	check.version = "v1.0.0"
	check.check = true
	check.yes = false
	if err := check.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "An update is available. Run `gridctl upgrade` to update.") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if got, _ := os.ReadFile(exe); string(got) != "current" {
		t.Fatal("check replaced the binary")
	}

	hits.reset()
	current, stdout, _ := testUpgrader(t, srv, exe)
	current.version = "v1.2.3"
	if err := current.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "gridctl is up to date.") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if hitPath(hits, "/releases/download/") {
		t.Fatal("up to date downloaded an archive")
	}

	abort, stdout, _ := testUpgrader(t, srv, exe)
	abort.version = "v1.0.0"
	abort.yes = false
	abort.isTTY = func() bool { return true }
	abort.stdin = strings.NewReader("n\n")
	if err := abort.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Aborted.") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if got, _ := os.ReadFile(exe); string(got) != "current" {
		t.Fatal("abort replaced the binary")
	}

	silent, _, _ := testUpgrader(t, srv, exe)
	silent.version = "v1.0.0"
	silent.yes = false
	if err := silent.run(t.Context()); err == nil || !strings.Contains(err.Error(), "pass --yes") {
		t.Fatalf("non-interactive err = %v", err)
	}
}

func TestUpgrade_RestartWhenCurrentSkipsSwap(t *testing.T) {
	payload := []byte("should-not-install")
	srv, hits := releaseServer(t, "v1.2.3", payload)
	logPath := filepath.Join(t.TempDir(), "args")
	script := filepath.Join(t.TempDir(), "gridctl")
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + shellQuote(logPath) + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	u, stdout, stderr := testUpgrader(t, srv, script)
	u.version = "v1.2.3"
	u.restart = true

	stateDir := filepath.Join(u.home, ".gridctl", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Port 0 makes the status read unread without a dial. The live PID
	// is what marks the daemon running.
	stateFile := fmt.Sprintf("{\"schema_version\":2,\"stack_name\":\"lab\",\"stack_file\":\"/tmp/lab.yaml\",\"pid\":%d,\"port\":0}\n", os.Getpid())
	if err := os.WriteFile(filepath.Join(stateDir, "lab.json"), []byte(stateFile), 0o600); err != nil {
		t.Fatal(err)
	}

	before, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if err := u.run(t.Context()); err != nil {
		t.Fatalf("restart: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	after, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("current-binary --restart replaced the executable")
	}
	if hitPath(hits, "/releases/download/") {
		t.Fatal("current-binary --restart downloaded an archive")
	}
	out := stdout.String()
	if !strings.Contains(out, "gridctl is up to date.") {
		t.Fatalf("stdout = %s", out)
	}
	if !strings.Contains(out, "Restarted lab") {
		t.Fatalf("stdout = %s\nstderr: %s", out, stderr.String())
	}
	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(logged)
	if !strings.Contains(text, "destroy lab") || !strings.Contains(text, "apply /tmp/lab.yaml --port 0") {
		t.Fatalf("args =\n%s", text)
	}
}

func TestUpgrade_ChecksumMismatchLeavesBinary(t *testing.T) {
	payload := []byte("real")
	srv, _ := releaseServerSum(t, "v1.2.3", payload, "deadbeef")
	exe := writeExe(t, []byte("old"))
	u, _, _ := testUpgrader(t, srv, exe)
	u.version = "v1.0.0"
	if err := u.run(t.Context()); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Fatal("checksum mismatch replaced the binary")
	}
}

func TestUpgradeRollbackFlagParsesOptionalTag(t *testing.T) {
	t.Cleanup(func() { resetUpgradeFlags(t) })
	resetUpgradeFlags(t)
	if err := upgradeCmd.Flags().Parse([]string{"--rollback", "v1.0.0"}); err != nil {
		t.Fatal(err)
	}
	got := upgradeRollback
	if got == "" || got == rollbackPrevious {
		args := upgradeCmd.Flags().Args()
		if len(args) == 1 {
			got = args[0]
		}
	}
	if got != "v1.0.0" {
		t.Fatalf("spaced tag = %q args = %v", upgradeRollback, upgradeCmd.Flags().Args())
	}
	resetUpgradeFlags(t)
	if err := upgradeCmd.Flags().Parse([]string{"--rollback=v1.2.3"}); err != nil {
		t.Fatal(err)
	}
	if upgradeRollback != "v1.2.3" {
		t.Fatalf("--rollback= value = %q", upgradeRollback)
	}
}

func TestUpgradeHelpAndMutualExclusion(t *testing.T) {
	out, _, err := executeCommand(t, "upgrade", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--rollback", "--list", "--keep", "--format", "--restart", "Exit codes:", "nothing retained"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q", want)
		}
	}
	resetUpgradeFlags(t)
	_, _, err = executeCommand(t, "upgrade", "--rollback", "--version", "v1.0.0")
	if err == nil || !strings.Contains(err.Error(), "rollback") {
		t.Fatalf("mutual exclusion err = %v", err)
	}
}

func resetUpgradeFlags(t *testing.T) {
	t.Helper()
	for _, name := range []string{"help", "rollback", "version", "check", "force", "yes", "list", "keep", "format", "restart"} {
		f := upgradeCmd.Flags().Lookup(name)
		if f == nil {
			continue
		}
		if err := f.Value.Set(f.DefValue); err != nil {
			t.Fatal(err)
		}
		f.Changed = false
	}
}

type hitLog struct {
	mu    sync.Mutex
	items []string
}

func (h *hitLog) add(path string) {
	h.mu.Lock()
	h.items = append(h.items, path)
	h.mu.Unlock()
}

func (h *hitLog) paths() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := append([]string(nil), h.items...)
	return out
}

func (h *hitLog) reset() {
	h.mu.Lock()
	h.items = nil
	h.mu.Unlock()
}

func releaseServer(t *testing.T, tag string, binary []byte) (*httptest.Server, *hitLog) {
	t.Helper()
	return releaseServerSum(t, tag, binary, "")
}

func releaseServerSum(t *testing.T, tag string, binary []byte, checksum string) (*httptest.Server, *hitLog) {
	t.Helper()
	archive := tarGz(t, binary)
	if checksum == "" {
		checksum = payloadSHA(t, archive)
	}
	versionNum := strings.TrimPrefix(normalizeTag(tag), "v")
	archiveName := fmt.Sprintf("gridctl_%s_%s_%s.tar.gz", versionNum, runtime.GOOS, runtime.GOARCH)
	hits := &hitLog{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.add(r.URL.Path)
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/gridctl/gridctl/releases"):
			if r.Header.Get("Accept") != "application/vnd.github+json" {
				t.Errorf("Accept = %q", r.Header.Get("Accept"))
			}
			if !strings.HasPrefix(r.Header.Get("User-Agent"), "gridctl/") {
				t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
			}
			fmt.Fprintf(w, `[{"tag_name":%q}]`, normalizeTag(tag))
		case strings.HasSuffix(r.URL.Path, "/"+archiveName):
			_, _ = w.Write(archive)
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			fmt.Fprintf(w, "%s  %s\n", checksum, archiveName)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

func testUpgrader(t *testing.T, srv *httptest.Server, exe string) (*upgrader, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	u := &upgrader{
		resolveExe: func() (string, error) { return exe, nil },
		client:     &http.Client{},
		stdin:      strings.NewReader(""),
		stdout:     stdout,
		stderr:     stderr,
		isTTY:      func() bool { return false },
		storeDir:   t.TempDir(),
		goos:       "linux",
		version:    "v1.0.0",
		yes:        true,
		keep:       2,
		format:     "text",
		home:       home,
	}
	if srv != nil {
		u.releaseAPI = srv.URL + "/repos/gridctl/gridctl/releases?per_page=1"
		u.downloadBase = srv.URL + "/gridctl/gridctl/releases/download"
	}
	return u, stdout, stderr
}

func writeExe(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gridctl")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func tarGz(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "gridctl", Mode: 0o755, Size: int64(len(payload))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func payloadSHA(t *testing.T, data []byte) string {
	t.Helper()
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hitPath(hits *hitLog, fragment string) bool {
	for _, path := range hits.paths() {
		if strings.Contains(path, fragment) {
			return true
		}
	}
	return false
}

func patchSchemas(t *testing.T, dir, tag string, schemas *versions.Schemas) {
	t.Helper()
	path := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var man versions.Manifest
	if err := json.Unmarshal(data, &man); err != nil {
		t.Fatal(err)
	}
	found := false
	for i := range man.Entries {
		if man.Entries[i].Tag == tag {
			man.Entries[i].Schemas = schemas
			found = true
		}
	}
	if !found {
		t.Fatalf("tag %s not in manifest", tag)
	}
	out, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustOpen(t *testing.T, dir string) *versions.Store {
	t.Helper()
	s, err := versions.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func entryTags(entries []versions.Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Tag
	}
	return out
}

func seedRetained(t *testing.T, dir, tag string, data []byte, previous bool) {
	t.Helper()
	s, err := versions.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	src := writeExe(t, data)
	if err := s.Retain(t.Context(), tag, src, versions.OriginDownloaded, nil); err != nil {
		t.Fatal(err)
	}
	prev := ""
	if previous {
		prev = tag
	}
	if err := s.SetActive(tag, prev); err != nil {
		t.Fatal(err)
	}
}
