package reload

import (
	"net"
	"slices"
	"strings"

	"github.com/gridctl/gridctl/pkg/config"
)

// NewSecurityPreflight freezes the resolved startup values and CLI overrides.
// The returned comparison reports field names only and never resolves the old
// side against a refreshed variable store.
func NewSecurityPreflight(startup *config.Stack, bind string, insecure bool) func(*config.Stack) []string {
	active := effectiveSecurity(startup, bind, insecure)
	return func(candidate *config.Stack) []string {
		next := effectiveSecurity(candidate, bind, insecure)
		var changed []string
		for _, field := range []string{
			"gateway.auth.enabled", "gateway.auth.type", "gateway.auth.token", "gateway.auth.header",
			"gateway.bind", "gateway.allowed_hosts", "gateway.allowed_origins", "gateway.insecure_allow_unauthenticated",
		} {
			if !slices.Equal(active[field], next[field]) {
				changed = append(changed, field)
			}
		}
		return changed
	}
}

// EffectiveBind applies the listener's CLI, stack, and loopback precedence.
func EffectiveBind(stack *config.Stack, override string) string {
	if override != "" {
		return override
	}
	if stack != nil && stack.Gateway != nil && stack.Gateway.Bind != "" {
		return stack.Gateway.Bind
	}
	return "127.0.0.1"
}

// AllowUnauthenticated applies the explicit CLI or stack insecure override.
func AllowUnauthenticated(stack *config.Stack, override bool) bool {
	return override || (stack != nil && stack.Gateway != nil && stack.Gateway.InsecureAllowUnauthenticated)
}

func effectiveSecurity(stack *config.Stack, bind string, insecure bool) map[string][]string {
	gateway := config.GatewayConfig{}
	if stack != nil && stack.Gateway != nil {
		gateway = *stack.Gateway
	}
	bind = EffectiveBind(stack, bind)
	if ip := net.ParseIP(bind); ip != nil {
		bind = ip.String()
	}
	result := map[string][]string{
		"gateway.bind":            {bind},
		"gateway.allowed_hosts":   securitySet(gateway.AllowedHosts, true),
		"gateway.allowed_origins": securitySet(gateway.AllowedOrigins, false),
	}
	if len(result["gateway.allowed_origins"]) == 0 {
		result["gateway.allowed_origins"] = []string{"*"}
	}
	if slices.Contains(result["gateway.allowed_origins"], "*") {
		result["gateway.allowed_origins"] = []string{"*"}
	}
	if AllowUnauthenticated(stack, insecure) {
		result["gateway.insecure_allow_unauthenticated"] = []string{"true"}
	}
	if auth := gateway.Auth; auth != nil && auth.Token != "" {
		result["gateway.auth.enabled"] = []string{"true"}
		result["gateway.auth.type"] = []string{auth.Type}
		result["gateway.auth.token"] = []string{auth.Token}
		header := strings.ToLower(auth.Header)
		if header == "" {
			header = "authorization"
		}
		result["gateway.auth.header"] = []string{header}
	}
	return result
}

func securitySet(values []string, fold bool) []string {
	result := slices.Clone(values)
	if fold {
		for i, value := range result {
			if host, _, err := net.SplitHostPort(value); err == nil {
				value = host
			}
			result[i] = strings.ToLower(value)
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}
