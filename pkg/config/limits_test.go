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
			name: "valid budget and rate entries",
			limits: &LimitsConfig{
				Budgets: []BudgetLimit{
					{Client: "claude-code", MaxUSD: 5, Period: "daily", WarnAtPercent: 80},
					{Server: "github", MaxUSD: 20, Period: "weekly"},
					{Tool: "github__search_code", MaxUSD: 1, Period: "monthly"},
				},
				RateLimits: []RateLimit{
					{Server: "github", CallsPerMinute: 30, Burst: 10},
					{Client: "cursor", CallsPerMinute: 6},
				},
			},
			wantErr: false,
		},
		{
			name: "no scope key",
			limits: &LimitsConfig{
				Budgets: []BudgetLimit{{MaxUSD: 5, Period: "daily"}},
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
				Budgets: []BudgetLimit{{Server: "nonexistent", MaxUSD: 5, Period: "daily"}},
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
				Budgets: []BudgetLimit{{Tool: "slack__post", MaxUSD: 5, Period: "daily"}},
			},
			wantErr: true,
			errMsg:  "references unknown MCP server 'slack'",
		},
		{
			name: "non-positive max_usd",
			limits: &LimitsConfig{
				Budgets: []BudgetLimit{{Client: "cursor", MaxUSD: 0, Period: "daily"}},
			},
			wantErr: true,
			errMsg:  "max_usd",
		},
		{
			name: "invalid period",
			limits: &LimitsConfig{
				Budgets: []BudgetLimit{{Client: "cursor", MaxUSD: 5, Period: "hourly"}},
			},
			wantErr: true,
			errMsg:  "must be 'daily', 'weekly', or 'monthly'",
		},
		{
			name: "warn_at_percent out of range",
			limits: &LimitsConfig{
				Budgets: []BudgetLimit{{Client: "cursor", MaxUSD: 5, Period: "daily", WarnAtPercent: 100}},
			},
			wantErr: true,
			errMsg:  "warn_at_percent",
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
			name: "duplicate budget scope",
			limits: &LimitsConfig{
				Budgets: []BudgetLimit{
					{Server: "github", MaxUSD: 5, Period: "daily"},
					{Server: "github", MaxUSD: 10, Period: "weekly"},
				},
			},
			wantErr: true,
			errMsg:  "duplicate budget",
		},
		{
			name: "duplicate client scope after slugging",
			limits: &LimitsConfig{
				Budgets: []BudgetLimit{
					{Client: "Claude Code", MaxUSD: 5, Period: "daily"},
					{Client: "claude-code", MaxUSD: 9, Period: "daily"},
				},
			},
			wantErr: true,
			errMsg:  "duplicate budget",
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
		{
			name: "same scope across budget and rate lists is fine",
			limits: &LimitsConfig{
				Budgets:    []BudgetLimit{{Server: "github", MaxUSD: 5, Period: "daily"}},
				RateLimits: []RateLimit{{Server: "github", CallsPerMinute: 30}},
			},
			wantErr: false,
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

func TestBudgetLimit_ScopeKey(t *testing.T) {
	tests := []struct {
		name     string
		budget   BudgetLimit
		wantKind string
		wantKey  string
		wantOK   bool
	}{
		{"client scope", BudgetLimit{Client: "claude-code"}, "client", "claude-code", true},
		{"server scope", BudgetLimit{Server: "github"}, "server", "github", true},
		{"tool scope", BudgetLimit{Tool: "github__search"}, "tool", "github__search", true},
		{"empty", BudgetLimit{}, "", "", false},
		{"two set", BudgetLimit{Client: "a", Tool: "b__c"}, "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, key, ok := tc.budget.ScopeKey()
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
// cycle without dropping fields (Article IX back-compat).
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
      warn_at_percent: 80
    - server: github
      max_usd: 20
      period: weekly
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
	if got := stack.Limits.Budgets[0]; got.Client != "claude-code" || got.MaxUSD != 5.5 || got.Period != "daily" || got.WarnAtPercent != 80 {
		t.Errorf("budgets[0] = %+v", got)
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
	if reparsed.Limits == nil || len(reparsed.Limits.Budgets) != 2 || len(reparsed.Limits.RateLimits) != 1 {
		t.Fatalf("round-trip lost entries: %+v", reparsed.Limits)
	}
}
