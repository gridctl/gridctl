package secrets

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/gridctl/gridctl/pkg/config"
)

const resolveParallelism = 4

// StackResolver resolves every secret reference in a stack. Tests stub it.
type StackResolver interface {
	ResolveStack(ctx context.Context, stack *config.Stack) (*config.Stack, map[string]error, []string, error)
	Values() map[string]string
}

// Resolver runs providers for one pass. The cache is per resolver, so a
// reload or retry builds a new one.
type Resolver struct {
	providers map[string]Provider
	mu        sync.Mutex
	cache     map[string]cached
	inflight  map[string]*flight
	sem       chan struct{}
	order     []string
	values    map[string]string
}

type cached struct {
	value string
	err   error
}

type flight struct {
	done  chan struct{}
	value string
	err   error
}

// NewResolver builds providers from the stack declaration and the daemon
// environment. Unknown types are omitted; a reference to one fails at resolve
// time as an undeclared provider.
func NewResolver(providers []config.SecretProvider, env []string) *Resolver {
	built := make(map[string]Provider, len(providers))
	for _, cfg := range providers {
		switch cfg.Type {
		case "exec":
			built[cfg.Name] = NewExec(cfg, env)
		case "onepassword":
			built[cfg.Name] = NewOnePassword(cfg, env)
		}
	}
	return &Resolver{
		providers: built,
		cache:     map[string]cached{},
		inflight:  map[string]*flight{},
		sem:       make(chan struct{}, resolveParallelism),
		values:    map[string]string{},
	}
}

// Resolve returns the value for one reference. Distinct references run at
// most four at a time. A repeated reference in the same resolver reuses the
// first result, including a failure, and does not start a second process.
func (r *Resolver) Resolve(ctx context.Context, ref Reference) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", contextCategory(ref, 0, err)
	}
	key := ref.String()
	r.mu.Lock()
	if ent, ok := r.cache[key]; ok {
		r.mu.Unlock()
		return ent.value, ent.err
	}
	if f, ok := r.inflight[key]; ok {
		r.mu.Unlock()
		select {
		case <-f.done:
			return f.value, f.err
		case <-ctx.Done():
			return "", contextCategory(ref, 0, ctx.Err())
		}
	}
	f := &flight{done: make(chan struct{})}
	r.inflight[key] = f
	r.mu.Unlock()

	value, err := r.resolveLimited(ctx, ref)
	r.mu.Lock()
	if !errors.Is(err, context.Canceled) {
		r.cache[key] = cached{value: value, err: err}
		if err == nil {
			if _, seen := r.values[key]; !seen {
				r.order = append(r.order, key)
			}
			r.values[key] = value
		}
	}
	delete(r.inflight, key)
	f.value, f.err = value, err
	close(f.done)
	r.mu.Unlock()
	return value, err
}

func (r *Resolver) resolveLimited(ctx context.Context, ref Reference) (string, error) {
	select {
	case <-ctx.Done():
		return "", contextCategory(ref, 0, ctx.Err())
	case r.sem <- struct{}{}:
	}
	defer func() { <-r.sem }()
	if err := ctx.Err(); err != nil {
		return "", contextCategory(ref, 0, err)
	}
	provider, ok := r.providers[ref.Provider]
	if !ok {
		e := secretError(ref, "undeclared provider", nil)
		slog.Warn("secret resolution failed", "provider", ref.Provider, "path", ref.Path, "category", e.Category)
		return "", e
	}
	return provider.Resolve(ctx, ref.Path)
}

// Values returns reference key to resolved value for this pass. The map is a
// copy. Failed references are absent.
func (r *Resolver) Values() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]string, len(r.values))
	for k, v := range r.values {
		out[k] = v
	}
	return out
}

// ResolveStack substitutes every secret reference in a deep copy. Failed
// servers and resources are removed. A gateway, network, or stack-name
// failure is returned under the key "stack" and as a non-nil error.
// The value list is successful resolutions in first-seen order.
func (r *Resolver) ResolveStack(ctx context.Context, stack *config.Stack) (*config.Stack, map[string]error, []string, error) {
	if stack == nil {
		return nil, map[string]error{}, nil, nil
	}
	if len(stack.SecretRefs) == 0 {
		return stack, map[string]error{}, nil, nil
	}
	var wg sync.WaitGroup
	for _, ref := range stack.SecretRefs {
		ref := ref
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.Resolve(ctx, Reference{Provider: ref.Provider, Path: ref.Path})
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, map[string]error{}, nil, contextCategory(Reference{}, 0, err)
	}

	failures := map[string]error{}
	var stackErr error
	for _, ref := range stack.SecretRefs {
		key := ref.Key
		r.mu.Lock()
		ent, ok := r.cache[key]
		r.mu.Unlock()
		if !ok || ent.err == nil {
			continue
		}
		sites := stack.References[key]
		if len(sites) == 0 {
			if _, seen := failures[stackFailureKey]; !seen {
				failures[stackFailureKey] = ent.err
				stackErr = ent.err
			}
			continue
		}
		for _, site := range sites {
			name, workload := workloadOf(site)
			if _, seen := failures[name]; seen {
				continue
			}
			failures[name] = ent.err
			if !workload && stackErr == nil {
				stackErr = ent.err
			}
		}
	}

	clone := stack.Clone()
	if clone == nil {
		return nil, failures, nil, secretError(Reference{}, "empty value", nil)
	}
	config.SubstituteSecretRefs(clone, r.Values())
	clone.MCPServers = filterServers(clone.MCPServers, failures)
	clone.Resources = filterResources(clone.Resources, failures)

	r.mu.Lock()
	values := make([]string, 0, len(r.order))
	for _, key := range r.order {
		if v, ok := r.values[key]; ok {
			values = append(values, v)
		}
	}
	r.mu.Unlock()
	return clone, failures, values, stackErr
}

func workloadOf(c config.Consumer) (string, bool) {
	switch c.Kind {
	case config.RefKindMCPServer, config.RefKindResource:
		return c.Name, true
	default:
		return stackFailureKey, false
	}
}

func filterServers(in []config.MCPServer, failures map[string]error) []config.MCPServer {
	out := make([]config.MCPServer, 0, len(in))
	for _, srv := range in {
		if _, failed := failures[srv.Name]; failed {
			continue
		}
		out = append(out, srv)
	}
	return out
}

func filterResources(in []config.Resource, failures map[string]error) []config.Resource {
	out := make([]config.Resource, 0, len(in))
	for _, res := range in {
		if _, failed := failures[res.Name]; failed {
			continue
		}
		out = append(out, res)
	}
	return out
}
