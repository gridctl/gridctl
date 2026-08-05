//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// conformanceVersion pins the suite so the checked-in expected-failures
// baseline describes a fixed scenario set; bump both together. Pinned to
// an exact 0.2.0-alpha version (never the floating `alpha` dist-tag):
// the alpha line is where 2026-07-28 support lives, and no 0.2.0 stable
// exists yet (move to it when tagged).
const conformanceVersion = "@modelcontextprotocol/conformance@0.2.0-alpha.10"

// conformanceSpecVersions are the --spec-version runs, one per supported
// protocol generation.
var conformanceSpecVersions = []string{"2025-11-25", "2026-07-28"}

// TestConformance runs the official MCP conformance suite against a
// live gridctl daemon, once per spec generation. The suite is the
// golden-message oracle; deviations gridctl accepts deliberately live
// in conformance-baseline.yml (stale entries fail the run, so the
// baseline cannot rot).
func TestConformance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	npx, err := exec.LookPath("npx")
	if err != nil {
		t.Skip("npx not available; conformance suite requires Node 22+")
	}
	if major := nodeMajorVersion(t); major > 0 && major < 22 {
		t.Skipf("conformance suite requires Node 22+, found major version %d", major)
	}

	// Build before redirecting HOME so `go build` resolves its real
	// module cache rather than poisoning the test's tempdir with
	// read-only mod-cache files that t.TempDir cleanup can't remove
	// (the serve_integration_test.go precedent).
	bin := buildGridctlBinary(t)
	isolateGridctlHome(t)
	port := freePort(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	serve := exec.CommandContext(ctx, bin, "serve", "--foreground", "--port", fmt.Sprintf("%d", port))
	serve.Stdout = os.Stderr
	serve.Stderr = os.Stderr
	if err := serve.Start(); err != nil {
		t.Fatalf("starting gridctl serve: %v", err)
	}
	t.Cleanup(func() {
		serve.Process.Kill() //nolint:errcheck
		serve.Wait()         //nolint:errcheck
	})
	waitForPort(t, ctx, port)

	baseline, err := filepath.Abs("conformance-baseline.yml")
	if err != nil {
		t.Fatal(err)
	}

	for _, specVersion := range conformanceSpecVersions {
		t.Run(specVersion, func(t *testing.T) {
			cmd := exec.CommandContext(ctx, npx, "--yes", conformanceVersion,
				"server",
				"--url", fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
				"--spec-version", specVersion,
				"--expected-failures", baseline,
			)
			cmd.Dir = t.TempDir() // keep the suite's results/ out of the repo
			out, err := cmd.CombinedOutput()
			t.Logf("conformance %s output:\n%s", specVersion, out)
			if err != nil {
				if _, isExit := err.(*exec.ExitError); !isExit {
					// npx itself failed (offline, registry unreachable):
					// environment, not conformance.
					t.Skipf("conformance suite could not run: %v", err)
				}
				t.Fatalf("conformance %s failed: %v", specVersion, err)
			}
		})
	}
}

// nodeMajorVersion parses `node --version` (e.g. "v22.23.1"); 0 when
// unknown, which lets the run proceed and fail with real output.
func nodeMajorVersion(t *testing.T) int {
	t.Helper()
	out, err := exec.Command("node", "--version").Output()
	if err != nil {
		return 0
	}
	version := strings.TrimPrefix(strings.TrimSpace(string(out)), "v")
	major, _, _ := strings.Cut(version, ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return 0
	}
	return n
}
