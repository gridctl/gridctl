package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestValidate_Limits(t *testing.T) {
	base := func(limits *LimitsConfig) *Stack {
		return &Stack{
			Name:    "test",
			Network: Network{Name: "net"},
			MCPServers: []MCPServer{
				{Name: "github", Image: "alpine", Port: 3000},
				{Name: "gitlab", Image: "alpine", Port: 3001},
			},
			Limits: limits,
		}
	}

	tests := []struct {
		name    string
		limits  *LimitsConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "no limits block is valid (back-compat)",
			limits:  nil,
			wantErr: false,
		},
		{
			name: "valid rate entries",
			limits: &LimitsConfig{
				RateLimits: []RateLimit{
					{Server: "github", CallsPerMinute: 30, Burst: 10},
					{Client: "cursor", CallsPerMinute: 6},
					{Tool: "github__search_code", CallsPerMinute: 12},
				},
			},
			wantErr: false,
		},
		{
			name: "no scope key",
			limits: &LimitsConfig{
				RateLimits: []RateLimit{{CallsPerMinute: 5}},
			},
			wantErr: true,
			errMsg:  "exactly one of 'client', 'server', or 'tool'",
		},
		{
			name: "two scope keys",
			limits: &LimitsConfig{
				RateLimits: []RateLimit{{Client: "cursor", Server: "github", CallsPerMinute: 5}},
			},
			wantErr: true,
			errMsg:  "exactly one of 'client', 'server', or 'tool'",
		},
		{
			name: "unknown server scope",
			limits: &LimitsConfig{
				RateLimits: []RateLimit{{Server: "nonexistent", CallsPerMinute: 5}},
			},
			wantErr: true,
			errMsg:  "unknown MCP server 'nonexistent'",
		},
		{
			name: "tool not prefixed",
			limits: &LimitsConfig{
				RateLimits: []RateLimit{{Tool: "search", CallsPerMinute: 5}},
			},
			wantErr: true,
			errMsg:  "must be a prefixed name",
		},
		{
			name: "tool references unknown server",
			limits: &LimitsConfig{
				RateLimits: []RateLimit{{Tool: "slack__post", CallsPerMinute: 5}},
			},
			wantErr: true,
			errMsg:  "references unknown MCP server 'slack'",
		},
		{
			name: "non-positive rate",
			limits: &LimitsConfig{
				RateLimits: []RateLimit{{Server: "github", CallsPerMinute: 0}},
			},
			wantErr: true,
			errMsg:  "calls_per_minute",
		},
		{
			name: "negative burst",
			limits: &LimitsConfig{
				RateLimits: []RateLimit{{Server: "github", CallsPerMinute: 5, Burst: -1}},
			},
			wantErr: true,
			errMsg:  "burst",
		},
		{
			name: "duplicate client scope after slugging",
			limits: &LimitsConfig{
				RateLimits: []RateLimit{
					{Client: "Claude Code", CallsPerMinute: 5},
					{Client: "claude-code", CallsPerMinute: 9},
				},
			},
			wantErr: true,
			errMsg:  "duplicate rate limit",
		},
		{
			name: "duplicate rate scope",
			limits: &LimitsConfig{
				RateLimits: []RateLimit{
					{Client: "cursor", CallsPerMinute: 5},
					{Client: "cursor", CallsPerMinute: 10},
				},
			},
			wantErr: true,
			errMsg:  "duplicate rate limit",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(base(tc.limits))
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

func TestRateLimit_ScopeKey(t *testing.T) {
	tests := []struct {
		name     string
		rate     RateLimit
		wantKind string
		wantKey  string
		wantOK   bool
	}{
		{"client scope", RateLimit{Client: "claude-code"}, "client", "claude-code", true},
		{"server scope", RateLimit{Server: "github"}, "server", "github", true},
		{"tool scope", RateLimit{Tool: "github__search"}, "tool", "github__search", true},
		{"empty", RateLimit{}, "", "", false},
		{"two set", RateLimit{Client: "a", Tool: "b__c"}, "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, key, ok := tc.rate.ScopeKey()
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && (kind != tc.wantKind || key != tc.wantKey) {
				t.Errorf("got (%q, %q), want (%q, %q)", kind, key, tc.wantKind, tc.wantKey)
			}
		})
	}
}

// TestLimitsConfig_RoundTrip asserts the limits block survives a load/save
// cycle without dropping fields (Article IX back-compat). A legacy budgets:
// block is ignored by the non-strict decoder rather than erroring.
func TestLimitsConfig_RoundTrip(t *testing.T) {
	src := `version: "1"
name: test
network:
  name: net
mcp-servers:
  - name: github
    image: alpine
    port: 3000
limits:
  budgets:
    - client: claude-code
      max_usd: 5.5
      period: daily
  rate_limits:
    - server: github
      calls_per_minute: 30
      burst: 10
`
	var stack Stack
	if err := yaml.Unmarshal([]byte(src), &stack); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if stack.Limits == nil {
		t.Fatal("limits block dropped on unmarshal")
	}
	if got := stack.Limits.RateLimits[0]; got.Server != "github" || got.CallsPerMinute != 30 || got.Burst != 10 {
		t.Errorf("rate_limits[0] = %+v", got)
	}

	out, err := yaml.Marshal(&stack)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var reparsed Stack
	if err := yaml.Unmarshal(out, &reparsed); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if reparsed.Limits == nil || len(reparsed.Limits.RateLimits) != 1 {
		t.Fatalf("round-trip lost entries: %+v", reparsed.Limits)
	}
}
