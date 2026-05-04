package config

import (
	"bytes"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func boolPtr(b bool) *bool { return &b }

func TestMCPServer_PersistResolvers(t *testing.T) {
	// Inheritance matrix: every cell of stack-global × per-server. Each
	// helper resolves the same way, so we exercise PersistLogs only and
	// then spot-check the other two.
	type cell struct {
		name         string
		stackPersist *bool // nil = no telemetry block on stack
		serverPersist *bool // nil = no override; serverTelemetryAbsent makes the whole block nil
		serverTelemetryAbsent bool
		want bool
	}

	cells := []cell{
		// Stack absent (Stack.Telemetry == nil).
		{name: "stack absent, server absent", serverTelemetryAbsent: true, want: false},
		{name: "stack absent, server inherit (nil override)", serverPersist: nil, want: false},
		{name: "stack absent, server explicit on", serverPersist: boolPtr(true), want: true},
		{name: "stack absent, server explicit off", serverPersist: boolPtr(false), want: false},

		// Stack global off.
		{name: "stack off, server absent", stackPersist: boolPtr(false), serverTelemetryAbsent: true, want: false},
		{name: "stack off, server inherit", stackPersist: boolPtr(false), want: false},
		{name: "stack off, server explicit on", stackPersist: boolPtr(false), serverPersist: boolPtr(true), want: true},
		{name: "stack off, server explicit off", stackPersist: boolPtr(false), serverPersist: boolPtr(false), want: false},

		// Stack global on.
		{name: "stack on, server absent", stackPersist: boolPtr(true), serverTelemetryAbsent: true, want: true},
		{name: "stack on, server inherit", stackPersist: boolPtr(true), want: true},
		{name: "stack on, server explicit on", stackPersist: boolPtr(true), serverPersist: boolPtr(true), want: true},
		{name: "stack on, server explicit off", stackPersist: boolPtr(true), serverPersist: boolPtr(false), want: false},
	}

	for _, c := range cells {
		t.Run(c.name, func(t *testing.T) {
			stack := &Stack{Name: "s"}
			if c.stackPersist != nil {
				stack.Telemetry = &TelemetryConfig{
					Persist: TelemetryPersistence{Logs: *c.stackPersist},
				}
			}
			server := &MCPServer{Name: "srv"}
			if !c.serverTelemetryAbsent {
				server.Telemetry = &MCPServerTelemetry{
					Persist: MCPServerPersistence{Logs: c.serverPersist},
				}
			}

			got := server.PersistLogs(stack)
			if got != c.want {
				t.Errorf("PersistLogs() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestMCPServer_PersistMetricsTracesParity(t *testing.T) {
	// Confirms PersistMetrics and PersistTraces share the same inheritance
	// shape as PersistLogs. We don't repeat the full matrix — one inherit
	// case + one explicit override per signal is enough.
	stack := &Stack{
		Name: "s",
		Telemetry: &TelemetryConfig{
			Persist: TelemetryPersistence{Logs: true, Metrics: true, Traces: true},
		},
	}
	server := &MCPServer{
		Name: "srv",
		Telemetry: &MCPServerTelemetry{
			Persist: MCPServerPersistence{
				Metrics: boolPtr(false), // explicit off overrides global on
				// Traces left nil — should inherit global on
			},
		},
	}

	if !server.PersistLogs(stack) {
		t.Error("PersistLogs: expected true (inherits global on), got false")
	}
	if server.PersistMetrics(stack) {
		t.Error("PersistMetrics: expected false (explicit override), got true")
	}
	if !server.PersistTraces(stack) {
		t.Error("PersistTraces: expected true (inherits global on), got false")
	}
}

func TestMCPServer_PersistResolvers_NilStack(t *testing.T) {
	// Defensive: nil stack and nil server must not panic and must return false.
	var server *MCPServer
	if server.PersistLogs(nil) {
		t.Error("nil server / nil stack: expected false, got true")
	}

	s := &MCPServer{Name: "srv", Telemetry: &MCPServerTelemetry{
		Persist: MCPServerPersistence{Logs: boolPtr(true)},
	}}
	if !s.PersistLogs(nil) {
		t.Error("explicit on with nil stack: expected true, got false")
	}
}

func TestMCPServer_PersistResolvers_EmptyTelemetryBlock(t *testing.T) {
	// Telemetry: &MCPServerTelemetry{} with all-nil Persist fields must
	// behave the same as no Telemetry block at all (every field inherits).
	stack := &Stack{Name: "s", Telemetry: &TelemetryConfig{
		Persist: TelemetryPersistence{Logs: true, Metrics: false, Traces: true},
	}}
	server := &MCPServer{Name: "srv", Telemetry: &MCPServerTelemetry{}}

	if !server.PersistLogs(stack) {
		t.Error("empty server.Telemetry must inherit stack.Logs (true), got false")
	}
	if server.PersistMetrics(stack) {
		t.Error("empty server.Telemetry must inherit stack.Metrics (false), got true")
	}
	if !server.PersistTraces(stack) {
		t.Error("empty server.Telemetry must inherit stack.Traces (true), got false")
	}
}

func TestStack_SetDefaults_TelemetryRetention(t *testing.T) {
	t.Run("no telemetry block: SetDefaults does not synthesize one", func(t *testing.T) {
		s := &Stack{Name: "s", Network: Network{Name: "n"}}
		s.SetDefaults()
		if s.Telemetry != nil {
			t.Errorf("Telemetry must remain nil, got %+v", s.Telemetry)
		}
	})

	t.Run("telemetry present, retention nil: defaults filled", func(t *testing.T) {
		s := &Stack{
			Name:      "s",
			Network:   Network{Name: "n"},
			Telemetry: &TelemetryConfig{Persist: TelemetryPersistence{Logs: true}},
		}
		s.SetDefaults()
		if s.Telemetry.Retention == nil {
			t.Fatal("Retention must be created")
		}
		if got := s.Telemetry.Retention.MaxSizeMB; got != 100 {
			t.Errorf("MaxSizeMB = %d, want 100", got)
		}
		if got := s.Telemetry.Retention.MaxBackups; got != 5 {
			t.Errorf("MaxBackups = %d, want 5", got)
		}
		if got := s.Telemetry.Retention.MaxAgeDays; got != 7 {
			t.Errorf("MaxAgeDays = %d, want 7", got)
		}
	})

	t.Run("telemetry present, retention partially set: only zero fields filled", func(t *testing.T) {
		s := &Stack{
			Name:    "s",
			Network: Network{Name: "n"},
			Telemetry: &TelemetryConfig{
				Retention: &RetentionConfig{MaxSizeMB: 250},
			},
		}
		s.SetDefaults()
		if s.Telemetry.Retention.MaxSizeMB != 250 {
			t.Errorf("MaxSizeMB = %d, want 250 (preserved)", s.Telemetry.Retention.MaxSizeMB)
		}
		if s.Telemetry.Retention.MaxBackups != 5 {
			t.Errorf("MaxBackups = %d, want 5 (defaulted)", s.Telemetry.Retention.MaxBackups)
		}
		if s.Telemetry.Retention.MaxAgeDays != 7 {
			t.Errorf("MaxAgeDays = %d, want 7 (defaulted)", s.Telemetry.Retention.MaxAgeDays)
		}
	})

	t.Run("idempotent: calling SetDefaults twice produces the same result", func(t *testing.T) {
		// SetDefaults runs from both LoadStack and the hot-reload path, so
		// double-application must be a no-op on the second pass.
		s := &Stack{
			Name:      "s",
			Network:   Network{Name: "n"},
			Telemetry: &TelemetryConfig{Persist: TelemetryPersistence{Logs: true}},
		}
		s.SetDefaults()
		first := *s.Telemetry.Retention
		s.SetDefaults()
		second := *s.Telemetry.Retention
		if !reflect.DeepEqual(first, second) {
			t.Errorf("SetDefaults not idempotent: first=%+v second=%+v", first, second)
		}
	})
}

func TestValidate_TelemetryRetention(t *testing.T) {
	base := func(r *RetentionConfig) *Stack {
		return &Stack{
			Name:       "test",
			Network:    Network{Name: "test-net"},
			MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000}},
			Telemetry:  &TelemetryConfig{Retention: r},
		}
	}

	tests := []struct {
		name    string
		stack   *Stack
		wantErr bool
		errMsg  string
	}{
		{
			name:    "no telemetry block — accepted",
			stack:   &Stack{Name: "test", Network: Network{Name: "n"}, MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000}}},
			wantErr: false,
		},
		{
			name:    "valid retention",
			stack:   base(&RetentionConfig{MaxSizeMB: 100, MaxBackups: 5, MaxAgeDays: 7}),
			wantErr: false,
		},
		{
			name:    "max_size_mb zero",
			stack:   base(&RetentionConfig{MaxSizeMB: 0, MaxBackups: 5, MaxAgeDays: 7}),
			wantErr: true,
			errMsg:  "telemetry.retention.max_size_mb",
		},
		{
			name:    "max_size_mb negative",
			stack:   base(&RetentionConfig{MaxSizeMB: -1, MaxBackups: 5, MaxAgeDays: 7}),
			wantErr: true,
			errMsg:  "telemetry.retention.max_size_mb",
		},
		{
			name:    "max_backups zero",
			stack:   base(&RetentionConfig{MaxSizeMB: 100, MaxBackups: 0, MaxAgeDays: 7}),
			wantErr: true,
			errMsg:  "telemetry.retention.max_backups",
		},
		{
			name:    "max_age_days zero",
			stack:   base(&RetentionConfig{MaxSizeMB: 100, MaxBackups: 5, MaxAgeDays: 0}),
			wantErr: true,
			errMsg:  "telemetry.retention.max_age_days",
		},
		{
			name:    "above 5GB soft cap — accepted with warning (not an error)",
			stack:   base(&RetentionConfig{MaxSizeMB: 2048, MaxBackups: 4, MaxAgeDays: 30}),
			wantErr: false,
		},
		{
			name:    "max_size_mb above hard cap",
			stack:   base(&RetentionConfig{MaxSizeMB: telemetryMaxSizeMBHardCap + 1, MaxBackups: 5, MaxAgeDays: 7}),
			wantErr: true,
			errMsg:  "telemetry.retention.max_size_mb",
		},
		{
			name:    "max_backups above hard cap",
			stack:   base(&RetentionConfig{MaxSizeMB: 100, MaxBackups: telemetryMaxBackupsHardCap + 1, MaxAgeDays: 7}),
			wantErr: true,
			errMsg:  "telemetry.retention.max_backups",
		},
		{
			name:    "max_age_days above hard cap",
			stack:   base(&RetentionConfig{MaxSizeMB: 100, MaxBackups: 5, MaxAgeDays: telemetryMaxAgeDaysHardCap + 1}),
			wantErr: true,
			errMsg:  "telemetry.retention.max_age_days",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.stack)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.errMsg != "" && !strings.Contains(err.Error(), tc.errMsg) {
					t.Errorf("expected error containing %q, got %q", tc.errMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestTelemetry_YAMLRoundTrip(t *testing.T) {
	// Source-of-truth YAML mirrors the schema example in the prompt. After
	// unmarshal → marshal → unmarshal, the *bool tri-state semantics must
	// survive untouched: nil stays nil, &false stays &false, &true stays &true.
	source := `version: "1"
name: my-stack
telemetry:
  persist:
    logs: true
    metrics: false
    traces: true
  retention:
    max_size_mb: 100
    max_backups: 5
    max_age_days: 7
network:
  name: my-stack-net
mcp-servers:
  - name: github
    image: alpine
    port: 3000
    telemetry:
      persist:
        traces: false
  - name: weather
    image: alpine
    port: 3001
  - name: explicit-on
    image: alpine
    port: 3002
    telemetry:
      persist:
        logs: true
        metrics: true
        traces: true
`

	var first Stack
	if err := yaml.Unmarshal([]byte(source), &first); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Spot-check parsed semantics on the first pass.
	if first.Telemetry == nil {
		t.Fatal("Telemetry block must parse")
	}
	if !first.Telemetry.Persist.Logs || first.Telemetry.Persist.Metrics || !first.Telemetry.Persist.Traces {
		t.Errorf("stack persist mismatch: %+v", first.Telemetry.Persist)
	}
	if first.Telemetry.Retention == nil || first.Telemetry.Retention.MaxSizeMB != 100 {
		t.Errorf("retention mismatch: %+v", first.Telemetry.Retention)
	}

	github := first.MCPServers[0]
	if github.Telemetry == nil {
		t.Fatal("github server must have a telemetry block")
	}
	if github.Telemetry.Persist.Logs != nil {
		t.Errorf("github.persist.logs must be nil (inherit), got %v", *github.Telemetry.Persist.Logs)
	}
	if github.Telemetry.Persist.Metrics != nil {
		t.Errorf("github.persist.metrics must be nil (inherit), got %v", *github.Telemetry.Persist.Metrics)
	}
	if github.Telemetry.Persist.Traces == nil || *github.Telemetry.Persist.Traces != false {
		t.Errorf("github.persist.traces must be &false, got %v", github.Telemetry.Persist.Traces)
	}

	weather := first.MCPServers[1]
	if weather.Telemetry != nil {
		t.Errorf("weather server must have nil Telemetry block, got %+v", weather.Telemetry)
	}

	explicit := first.MCPServers[2]
	if explicit.Telemetry == nil {
		t.Fatal("explicit-on server must have a telemetry block")
	}
	if explicit.Telemetry.Persist.Logs == nil || !*explicit.Telemetry.Persist.Logs {
		t.Errorf("explicit-on.persist.logs must be &true")
	}

	// Round-trip: marshal then unmarshal again and confirm pointer semantics
	// survived. yaml.v3 must not collapse nil → &false or vice-versa.
	out, err := yaml.Marshal(&first)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var second Stack
	if err := yaml.Unmarshal(out, &second); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}

	// Round-trip equivalence on the stack-global block.
	if second.Telemetry == nil {
		t.Fatal("second.Telemetry must survive round-trip")
	}
	if !reflect.DeepEqual(second.Telemetry.Persist, first.Telemetry.Persist) {
		t.Errorf("stack persist round-trip mismatch: got %+v want %+v",
			second.Telemetry.Persist, first.Telemetry.Persist)
	}

	// Round-trip equivalence on the per-server tri-state.
	for i, server := range second.MCPServers {
		want := first.MCPServers[i]
		if (server.Telemetry == nil) != (want.Telemetry == nil) {
			t.Errorf("server[%s]: Telemetry presence mismatch after round-trip", server.Name)
			continue
		}
		if server.Telemetry == nil {
			continue
		}
		if !boolPtrEqual(server.Telemetry.Persist.Logs, want.Telemetry.Persist.Logs) {
			t.Errorf("server[%s].persist.logs round-trip: got %v, want %v",
				server.Name, derefOrNil(server.Telemetry.Persist.Logs), derefOrNil(want.Telemetry.Persist.Logs))
		}
		if !boolPtrEqual(server.Telemetry.Persist.Metrics, want.Telemetry.Persist.Metrics) {
			t.Errorf("server[%s].persist.metrics round-trip mismatch", server.Name)
		}
		if !boolPtrEqual(server.Telemetry.Persist.Traces, want.Telemetry.Persist.Traces) {
			t.Errorf("server[%s].persist.traces round-trip mismatch", server.Name)
		}
	}
}

func TestValidate_TelemetryRetention_SoftCapWarning(t *testing.T) {
	// Capture slog output and assert exactly one warning is emitted when the
	// per-server worst case crosses the soft cap. Future refactors that drop
	// the warning will fail this test.
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	stack := &Stack{
		Name:       "test",
		Network:    Network{Name: "test-net"},
		MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000}},
		Telemetry: &TelemetryConfig{
			Retention: &RetentionConfig{MaxSizeMB: 2048, MaxBackups: 4, MaxAgeDays: 30},
		},
	}
	if err := Validate(stack); err != nil {
		t.Fatalf("Validate must accept above-soft-cap retention, got %v", err)
	}
	if !strings.Contains(buf.String(), "telemetry retention exceeds soft cap") {
		t.Errorf("expected soft-cap warning in slog output, got: %q", buf.String())
	}

	// Below the cap must not warn.
	buf.Reset()
	stack.Telemetry.Retention = &RetentionConfig{MaxSizeMB: 100, MaxBackups: 5, MaxAgeDays: 7}
	if err := Validate(stack); err != nil {
		t.Fatalf("Validate must accept default retention: %v", err)
	}
	if strings.Contains(buf.String(), "soft cap") {
		t.Errorf("did not expect soft-cap warning for default retention, got: %q", buf.String())
	}
}

func TestTelemetry_NoBlockBackwardsCompatible(t *testing.T) {
	// A stack file with no telemetry block must parse with Stack.Telemetry == nil
	// and resolver helpers returning false for every server. This guards the
	// "default off in beta" acceptance criterion.
	source := `version: "1"
name: legacy
network:
  name: legacy-net
mcp-servers:
  - name: github
    image: alpine
    port: 3000
`
	var stack Stack
	if err := yaml.Unmarshal([]byte(source), &stack); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	stack.SetDefaults()

	if stack.Telemetry != nil {
		t.Errorf("Stack.Telemetry must be nil for legacy stacks, got %+v", stack.Telemetry)
	}
	server := &stack.MCPServers[0]
	if server.PersistLogs(&stack) || server.PersistMetrics(&stack) || server.PersistTraces(&stack) {
		t.Error("legacy stack: every persist resolver must return false")
	}
}

func boolPtrEqual(a, b *bool) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func derefOrNil(p *bool) interface{} {
	if p == nil {
		return nil
	}
	return *p
}
