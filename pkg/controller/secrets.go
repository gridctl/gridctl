package controller

import (
	"context"
	"fmt"
	"os"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/secrets"
)

// deploySecrets is the literal/resolved pair for one apply or daemon-child
// start. resolved equals literal when the stack has no secret references.
type deploySecrets struct {
	literal  *config.Stack
	resolved *config.Stack
	failures map[string]error
	values   []string
	byRef    map[string]string
}

func resolveDeploy(ctx context.Context, literal *config.Stack) (*deploySecrets, error) {
	out := &deploySecrets{literal: literal, resolved: literal}
	if literal == nil || len(literal.SecretRefs) == 0 {
		return out, nil
	}
	var providers []config.SecretProvider
	if literal.Secrets != nil {
		providers = literal.Secrets.Providers
	}
	resolver := secrets.NewResolver(providers, os.Environ())
	resolved, failures, values, err := resolver.ResolveStack(ctx, literal)
	logging.RegisterSensitiveValues(values...)
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		resolved = literal
	}
	out.resolved = resolved
	out.failures = failures
	out.values = values
	out.byRef = resolver.Values()
	return out, nil
}

func printSecretFailures(printer *output.Printer, failures map[string]error) {
	if len(failures) == 0 {
		return
	}
	names := make([]string, 0, len(failures))
	for name := range failures {
		if name == "" || name == "stack" {
			continue
		}
		names = append(names, name)
	}
	sortStrings(names)
	for _, name := range names {
		msg := fmt.Sprintf("%s: %s", name, failures[name].Error())
		if printer != nil {
			printer.Error(msg)
			continue
		}
		fmt.Fprintln(os.Stderr, msg)
	}
}

func sortStrings(in []string) {
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j] < in[j-1]; j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
}

func (b *GatewayBuilder) SetResolved(stack *config.Stack, values map[string]string, unresolved map[string]error) {
	b.storeResolved(stack)
	b.secretValues = values
	b.secretUnresolved = unresolved
	logging.RegisterSensitiveValues(valuesSlice(values)...)
}

func (b *GatewayBuilder) storeResolved(stack *config.Stack) {
	if stack == nil {
		return
	}
	b.resolved.Store(stack)
}

func valuesSlice(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, v)
	}
	return out
}

func (b *GatewayBuilder) listenerStack() *config.Stack {
	if resolved := b.resolved.Load(); resolved != nil {
		return resolved
	}
	return b.stack
}

// applyGatewayAuth installs listener credentials from the resolved stack.
// The applied-config hook must keep assigning the literal stack elsewhere.
func applyGatewayAuth(stack *config.Stack, set func(authType, token, header string)) {
	if set == nil || stack == nil || stack.Gateway == nil || stack.Gateway.Auth == nil {
		return
	}
	auth := stack.Gateway.Auth
	set(auth.Type, auth.Token, auth.Header)
}

func (b *GatewayBuilder) recordStartupSecretFailures(gateway *mcp.Gateway) {
	if gateway == nil {
		return
	}
	for name, err := range b.secretUnresolved {
		if name == "" || name == "stack" || err == nil || !stackHasServer(b.stack, name) {
			continue
		}
		gateway.RecordRegistrationFailure(name, err)
	}
}

func stackHasServer(stack *config.Stack, name string) bool {
	if stack == nil {
		return false
	}
	for _, srv := range stack.MCPServers {
		if srv.Name == name {
			return true
		}
	}
	return false
}
