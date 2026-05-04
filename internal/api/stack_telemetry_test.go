package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// telemetryFixture is the shared fixture for patcher unit tests. It carries:
//   - a top-of-file comment
//   - inline comments inside mcp-servers
//   - a stack-global telemetry block with persist + retention pre-populated
//   - one mcp-server with an existing telemetry override and one without
//   - non-canonical key order (`transport: http` before `url:`) so a struct
//     round-trip would visibly reshuffle it
const telemetryFixture = `# top-of-file comment — must survive round-trip
version: "1"
name: example
network:
  name: example-net
  driver: bridge
telemetry:
  persist:
    logs: true
    metrics: false
    traces: true
  retention:
    max_size_mb: 100
    max_backups: 5
    max_age_days: 7
mcp-servers:
  - name: github
    transport: http
    url: https://api.github.com/mcp
    telemetry:
      persist:
        traces: false # explicit override
  - name: filesystem
    transport: stdio
    command: gridctl-fs
`

func boolPtr(b bool) *bool { return &b }
func intPtr(i int) *int    { return &i }

func TestPatchStackTelemetry_PreservesCommentsAndOrder(t *testing.T) {
	out, err := patchStackTelemetry([]byte(telemetryFixture),
		&stackPersistDelta{Logs: boolPtr(false)},
		nil,
	)
	require.NoError(t, err)
	got := string(out)

	assert.Contains(t, got, "# top-of-file comment")
	assert.Contains(t, got, "explicit override")

	// transport must remain before url for github even after the unrelated
	// stack-global telemetry mutation.
	transportIdx := strings.Index(got, "transport: http")
	urlIdx := strings.Index(got, "url: https://api.github.com/mcp")
	require.Greater(t, transportIdx, 0)
	require.Greater(t, urlIdx, 0)
	assert.Less(t, transportIdx, urlIdx)

	// logs flipped to false, metrics/traces unchanged from fixture.
	assert.Regexp(t, `(?m)^\s*logs:\s*false`, got)
	assert.Regexp(t, `(?m)^\s*metrics:\s*false`, got)
	assert.Regexp(t, `(?m)^\s*traces:\s*true`, got)

	// Retention is untouched because retention delta is nil.
	assert.Contains(t, got, "max_size_mb: 100")
	assert.Contains(t, got, "max_backups: 5")
	assert.Contains(t, got, "max_age_days: 7")
}

func TestPatchStackTelemetry_RetentionOnly(t *testing.T) {
	out, err := patchStackTelemetry([]byte(telemetryFixture),
		nil,
		&retentionDelta{MaxAgeDays: intPtr(30)},
	)
	require.NoError(t, err)
	got := string(out)

	// Persist subblock untouched.
	assert.Regexp(t, `(?m)^\s*logs:\s*true`, got)
	// Retention max_age_days bumped, others preserved.
	assert.Contains(t, got, "max_age_days: 30")
	assert.Contains(t, got, "max_size_mb: 100")
	assert.Contains(t, got, "max_backups: 5")
}

func TestPatchStackTelemetry_CreatesBlockOnEmptyStack(t *testing.T) {
	source := `# header
version: "1"
name: empty
mcp-servers: []
`
	out, err := patchStackTelemetry([]byte(source),
		&stackPersistDelta{Logs: boolPtr(true), Metrics: boolPtr(true)},
		&retentionDelta{MaxSizeMB: intPtr(50)},
	)
	require.NoError(t, err)
	got := string(out)

	assert.Contains(t, got, "# header")
	assert.Contains(t, got, "telemetry:")
	assert.Contains(t, got, "persist:")
	assert.Regexp(t, `(?m)logs:\s*true`, got)
	assert.Regexp(t, `(?m)metrics:\s*true`, got)
	assert.Contains(t, got, "max_size_mb: 50")
}

func TestPatchStackTelemetry_Idempotent(t *testing.T) {
	delta := &stackPersistDelta{Logs: boolPtr(true), Metrics: boolPtr(false), Traces: boolPtr(true)}
	first, err := patchStackTelemetry([]byte(telemetryFixture), delta, nil)
	require.NoError(t, err)
	second, err := patchStackTelemetry(first, delta, nil)
	require.NoError(t, err)
	assert.Equal(t, string(first), string(second))
}

func TestPatchServerTelemetry_SetExplicitOverride(t *testing.T) {
	out, err := patchServerTelemetry([]byte(telemetryFixture), "filesystem",
		serverPersistDelta{Logs: serverPersistTrue, Metrics: serverPersistFalse},
		false,
	)
	require.NoError(t, err)
	got := string(out)

	// Comments survive.
	assert.Contains(t, got, "# top-of-file comment")
	assert.Contains(t, got, "explicit override")

	// New filesystem.telemetry block written; github untouched.
	fsIdx := strings.Index(got, "name: filesystem")
	require.Greater(t, fsIdx, 0)
	tail := got[fsIdx:]
	assert.Contains(t, tail, "telemetry:")
	assert.Contains(t, tail, "persist:")
	assert.Regexp(t, `(?m)^\s*logs:\s*true`, tail)
	assert.Regexp(t, `(?m)^\s*metrics:\s*false`, tail)
	assert.NotContains(t, tail, "traces:") // not specified, must not appear
}

func TestPatchServerTelemetry_ClearSingleSignalRemovesKey(t *testing.T) {
	// github has `traces: false` in the fixture; clearing it should remove
	// the traces key, then collapse the empty persist mapping, then collapse
	// the empty telemetry mapping.
	out, err := patchServerTelemetry([]byte(telemetryFixture), "github",
		serverPersistDelta{Traces: serverPersistClear},
		false,
	)
	require.NoError(t, err)
	got := string(out)

	githubIdx := strings.Index(got, "name: github")
	fsIdx := strings.Index(got, "name: filesystem")
	require.Greater(t, githubIdx, 0)
	require.Greater(t, fsIdx, githubIdx)
	githubBlock := got[githubIdx:fsIdx]

	// Whole telemetry block under github is gone because it had only this
	// override.
	assert.NotContains(t, githubBlock, "telemetry:")
	assert.NotContains(t, githubBlock, "persist:")
	assert.NotContains(t, githubBlock, "traces:")
}

func TestPatchServerTelemetry_ClearAllRemovesBlock(t *testing.T) {
	out, err := patchServerTelemetry([]byte(telemetryFixture), "github",
		serverPersistDelta{}, // ignored when clearAll=true
		true,
	)
	require.NoError(t, err)
	got := string(out)

	githubIdx := strings.Index(got, "name: github")
	fsIdx := strings.Index(got, "name: filesystem")
	require.Greater(t, githubIdx, 0)
	require.Greater(t, fsIdx, githubIdx)
	githubBlock := got[githubIdx:fsIdx]

	assert.NotContains(t, githubBlock, "telemetry:")

	// Filesystem entry is unchanged.
	assert.Contains(t, got, "command: gridctl-fs")
}

func TestPatchServerTelemetry_NotFound(t *testing.T) {
	_, err := patchServerTelemetry([]byte(telemetryFixture), "nope",
		serverPersistDelta{Logs: serverPersistTrue}, false,
	)
	assert.ErrorIs(t, err, errServerNotFound)
}

func TestPatchServerTelemetry_PartialUpdatePreservesOthers(t *testing.T) {
	// github currently has `traces: false`. Setting logs:true should add it
	// without disturbing the existing traces override.
	out, err := patchServerTelemetry([]byte(telemetryFixture), "github",
		serverPersistDelta{Logs: serverPersistTrue},
		false,
	)
	require.NoError(t, err)
	got := string(out)

	githubIdx := strings.Index(got, "name: github")
	fsIdx := strings.Index(got, "name: filesystem")
	require.Greater(t, fsIdx, githubIdx)
	githubBlock := got[githubIdx:fsIdx]

	assert.Contains(t, githubBlock, "telemetry:")
	assert.Regexp(t, `(?m)^\s*logs:\s*true`, githubBlock)
	assert.Regexp(t, `(?m)^\s*traces:\s*false`, githubBlock)
}

func TestParseServerPersist(t *testing.T) {
	type want struct {
		delta    serverPersistDelta
		clearAll bool
		errMatch string
	}
	cases := []struct {
		name string
		in   string
		want want
	}{
		{"empty", ``, want{}},
		{"null", `null`, want{clearAll: true}},
		{"empty object", `{}`, want{}},
		{"all true", `{"logs":true,"metrics":true,"traces":true}`,
			want{delta: serverPersistDelta{Logs: serverPersistTrue, Metrics: serverPersistTrue, Traces: serverPersistTrue}}},
		{"mixed clear", `{"logs":true,"metrics":null}`,
			want{delta: serverPersistDelta{Logs: serverPersistTrue, Metrics: serverPersistClear}}},
		{"explicit false", `{"traces":false}`,
			want{delta: serverPersistDelta{Traces: serverPersistFalse}}},
		{"bad type", `{"logs":"yes"}`, want{errMatch: "persist.logs"}},
		{"bad shape", `"hello"`, want{errMatch: "persist must be an object"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			delta, clearAll, err := parseServerPersist([]byte(tc.in))
			if tc.want.errMatch != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.want.errMatch)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want.clearAll, clearAll)
			assert.Equal(t, tc.want.delta, delta)
		})
	}
}
