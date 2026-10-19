package reload

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/secrets"
)

// SetSecretResolverFactory installs the per-pass resolver. Nil keeps reload
// from running providers. Plan never calls it.
func (h *Handler) SetSecretResolverFactory(factory func(*config.Stack) secrets.StackResolver) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.secretFactory = factory
}

// SeedResolved records the startup pass so the first reload does not treat
// every secret as rotated. A missing key later is a seed, not a rotation.
func (h *Handler) SeedResolved(values map[string]string, unresolved map[string]error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.resolvedValues = copyStringMap(values)
	h.resolvedAt = map[string]time.Time{}
	now := time.Now().UTC()
	for key := range h.resolvedValues {
		h.resolvedAt[key] = now
	}
	h.unresolved = copyErrorMap(unresolved)
}

// SetOnResolvedApplied receives the resolved stack after a successful pass.
// It must not rebuild that stack from the literal configuration.
func (h *Handler) SetOnResolvedApplied(fn func(*config.Stack)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onResolvedApplied = fn
}

// HasUnresolved reports whether a secret failure is waiting to be retried.
func (h *Handler) HasUnresolved() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.unresolved) > 0
}

// SecretObservation is the value-free view GET /api/secrets reads.
type SecretObservation struct {
	Resolved   map[string]string
	ResolvedAt map[string]time.Time
	Unresolved map[string]error
	Seeded     bool
}

// SecretObservation returns copies of the in-memory resolution maps.
func (h *Handler) SecretObservation() SecretObservation {
	h.mu.Lock()
	defer h.mu.Unlock()
	return SecretObservation{
		Resolved:   copyStringMap(h.resolvedValues),
		ResolvedAt: copyTimeMap(h.resolvedAt),
		Unresolved: copyErrorMap(h.unresolved),
		Seeded:     h.resolvedValues != nil || h.unresolved != nil,
	}
}

type secretPass struct {
	resolved *config.Stack
	failures map[string]error
	values   []string
	byRef    map[string]string
	err      error
}

func (h *Handler) resolveCandidate(ctx context.Context, literal *config.Stack) secretPass {
	pass := secretPass{resolved: literal}
	if h.secretFactory == nil || literal == nil || len(literal.SecretRefs) == 0 {
		return pass
	}
	resolver := h.secretFactory(literal)
	if resolver == nil {
		return pass
	}
	resolved, failures, values, err := resolver.ResolveStack(ctx, literal)
	logging.RegisterSensitiveValues(values...)
	pass.failures = failures
	pass.values = values
	pass.err = err
	pass.byRef = resolver.Values()
	if resolved != nil {
		pass.resolved = resolved
	}
	return pass
}

func (h *Handler) noteRotation(diff *ConfigDiff, literal, resolved *config.Stack, byRef map[string]string) map[string]bool {
	rotated := map[string]bool{}
	if h.currentCfg == nil || literal == nil || len(byRef) == 0 {
		return rotated
	}
	prevServers := map[string]config.MCPServer{}
	for _, srv := range h.currentCfg.MCPServers {
		prevServers[srv.Name] = srv
	}
	for _, srv := range literal.MCPServers {
		old, ok := prevServers[srv.Name]
		if !ok || !config.MCPServerEqual(old, srv) {
			continue
		}
		if !secretRotated(refsFor(literal, config.RefKindMCPServer, srv.Name), h.resolvedValues, byRef) {
			continue
		}
		next := srv
		if found, ok := serverByName(resolved, srv.Name); ok {
			next = found
		}
		diff.MCPServers.Modified = append(diff.MCPServers.Modified, MCPServerChange{Name: srv.Name, Old: old, New: next})
		rotated["mcp-server:"+srv.Name] = true
	}
	prevResources := map[string]config.Resource{}
	for _, res := range h.currentCfg.Resources {
		prevResources[res.Name] = res
	}
	for _, res := range literal.Resources {
		old, ok := prevResources[res.Name]
		if !ok || !resourceEqual(old, res) {
			continue
		}
		if !secretRotated(refsFor(literal, config.RefKindResource, res.Name), h.resolvedValues, byRef) {
			continue
		}
		next := res
		if found, ok := resourceByName(resolved, res.Name); ok {
			next = found
		}
		diff.Resources.Modified = append(diff.Resources.Modified, ResourceChange{Name: res.Name, Old: old, New: next})
		rotated["resource:"+res.Name] = true
	}
	return rotated
}

func secretRotated(refs []config.SecretRef, previous, next map[string]string) bool {
	for _, ref := range refs {
		newVal, ok := next[ref.Key]
		if !ok {
			continue
		}
		oldVal, seen := previous[ref.Key]
		if !seen {
			continue
		}
		if oldVal != newVal {
			return true
		}
	}
	return false
}

func (h *Handler) noteRecovery(diff *ConfigDiff, literal, resolved *config.Stack, failures map[string]error) {
	if len(h.unresolved) == 0 {
		return
	}
	for name := range h.unresolved {
		if _, failed := failures[name]; failed {
			continue
		}
		if srv, ok := serverByName(resolved, name); ok && !serverInDiff(diff, name) {
			if _, declared := serverByName(literal, name); declared {
				diff.MCPServers.Added = append(diff.MCPServers.Added, srv)
			}
			continue
		}
		if res, ok := resourceByName(resolved, name); ok && !resourceInDiff(diff, name) {
			if _, declared := resourceByName(literal, name); declared {
				diff.Resources.Added = append(diff.Resources.Added, res)
			}
		}
	}
}

func (h *Handler) dropFailed(diff *ConfigDiff, failures map[string]error) {
	if len(failures) == 0 {
		return
	}
	diff.MCPServers.Added = filterServers(diff.MCPServers.Added, failures)
	diff.MCPServers.Enabled = filterServers(diff.MCPServers.Enabled, failures)
	diff.MCPServers.Modified = filterServerChanges(diff.MCPServers.Modified, failures)
	diff.Resources.Added = filterResources(diff.Resources.Added, failures)
	diff.Resources.Enabled = filterResources(diff.Resources.Enabled, failures)
	diff.Resources.Modified = filterResourceChanges(diff.Resources.Modified, failures)
}

func (h *Handler) rewriteResolved(diff *ConfigDiff, resolved *config.Stack) {
	if resolved == nil {
		return
	}
	for i, srv := range diff.MCPServers.Added {
		if next, ok := serverByName(resolved, srv.Name); ok {
			diff.MCPServers.Added[i] = next
		}
	}
	for i, srv := range diff.MCPServers.Enabled {
		if next, ok := serverByName(resolved, srv.Name); ok {
			diff.MCPServers.Enabled[i] = next
		}
	}
	for i, change := range diff.MCPServers.Modified {
		if next, ok := serverByName(resolved, change.Name); ok {
			diff.MCPServers.Modified[i].New = next
		}
	}
	for i, res := range diff.Resources.Added {
		if next, ok := resourceByName(resolved, res.Name); ok {
			diff.Resources.Added[i] = next
		}
	}
	for i, res := range diff.Resources.Enabled {
		if next, ok := resourceByName(resolved, res.Name); ok {
			diff.Resources.Enabled[i] = next
		}
	}
	for i, change := range diff.Resources.Modified {
		if next, ok := resourceByName(resolved, change.Name); ok {
			diff.Resources.Modified[i].New = next
		}
	}
}

func (h *Handler) commitSecretPass(byRef map[string]string, failures map[string]error) {
	if byRef == nil && failures == nil {
		return
	}
	h.resolvedValues = copyStringMap(byRef)
	if h.resolvedAt == nil {
		h.resolvedAt = map[string]time.Time{}
	}
	now := time.Now().UTC()
	nextAt := map[string]time.Time{}
	for key := range h.resolvedValues {
		if prev, ok := h.resolvedAt[key]; ok && byRef[key] == "" {
			nextAt[key] = prev
			continue
		}
		nextAt[key] = now
	}
	h.resolvedAt = nextAt
	next := map[string]error{}
	for name, err := range failures {
		if name == "" || name == "stack" || err == nil {
			continue
		}
		next[name] = err
	}
	h.unresolved = next
}

func (h *Handler) recordSecretFailures(literal *config.Stack, result *ReloadResult, failures map[string]error) {
	if len(failures) == 0 {
		return
	}
	names := make([]string, 0, len(failures))
	for name := range failures {
		if name != "" && name != "stack" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		err := failures[name]
		if result != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", name, err.Error()))
		}
		if h.gateway != nil {
			if _, ok := serverByName(literal, name); ok {
				h.gateway.RecordRegistrationFailure(name, err)
			}
		}
	}
}

func labelRotated(result *ReloadResult, rotated map[string]bool) {
	if result == nil || len(rotated) == 0 {
		return
	}
	for i, item := range result.Modified {
		if rotated[item] {
			result.Modified[i] = item + " (secret rotated)"
		}
	}
}

// RetrySecrets re-resolves each unresolved workload with a fresh resolver.
// Success starts and registers it. Failure keeps the entry and updates the
// recorded message.
func (h *Handler) RetrySecrets(ctx context.Context) (recovered []string, still []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.unresolved) == 0 || h.secretFactory == nil || h.currentCfg == nil {
		return nil, nil
	}
	names := make([]string, 0, len(h.unresolved))
	for name := range h.unresolved {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			still = append(still, names...)
			return recovered, still
		}
		if h.retryWorkload(ctx, name) {
			recovered = append(recovered, name)
			delete(h.unresolved, name)
			if h.gateway != nil {
				h.gateway.ClearRegistrationFailure(name)
			}
			continue
		}
		still = append(still, name)
	}
	return recovered, still
}

func (h *Handler) retryWorkload(ctx context.Context, name string) bool {
	mini := &config.Stack{
		Name:       h.currentCfg.Name,
		Secrets:    h.currentCfg.Secrets,
		References: h.currentCfg.References,
		Network:    h.currentCfg.Network,
		Networks:   append([]config.Network(nil), h.currentCfg.Networks...),
		SecretRefs: append(refsFor(h.currentCfg, config.RefKindMCPServer, name), refsFor(h.currentCfg, config.RefKindResource, name)...),
	}
	var wantServer bool
	for _, srv := range h.currentCfg.MCPServers {
		if srv.Name == name {
			mini.MCPServers = []config.MCPServer{srv}
			wantServer = true
			break
		}
	}
	if !wantServer {
		for _, res := range h.currentCfg.Resources {
			if res.Name == name {
				mini.Resources = []config.Resource{res}
				break
			}
		}
	}
	if len(mini.MCPServers) == 0 && len(mini.Resources) == 0 {
		return false
	}
	resolver := h.secretFactory(mini)
	if resolver == nil {
		return false
	}
	resolved, failures, values, err := resolver.ResolveStack(ctx, mini)
	logging.RegisterSensitiveValues(values...)
	if err != nil || failures[name] != nil {
		if failures[name] != nil {
			h.unresolved[name] = failures[name]
			if h.gateway != nil && wantServer {
				h.gateway.RecordRegistrationFailure(name, failures[name])
			}
		}
		return false
	}
	for key, value := range resolver.Values() {
		if h.resolvedValues == nil {
			h.resolvedValues = map[string]string{}
		}
		h.resolvedValues[key] = value
		if h.resolvedAt == nil {
			h.resolvedAt = map[string]time.Time{}
		}
		h.resolvedAt[key] = time.Now().UTC()
	}
	if wantServer {
		srv, ok := serverByName(resolved, name)
		if !ok {
			return false
		}
		image, prepErr := h.prepareMCPServer(ctx, srv, resolved)
		if prepErr != nil {
			h.unresolved[name] = prepErr
			if h.gateway != nil {
				h.gateway.RecordRegistrationFailure(name, prepErr)
			}
			return false
		}
		if startErr := h.startMCPServer(ctx, srv, resolved, image); startErr != nil {
			h.unresolved[name] = startErr
			if h.gateway != nil {
				h.gateway.RecordRegistrationFailure(name, startErr)
			}
			return false
		}
		return true
	}
	res, ok := resourceByName(resolved, name)
	if !ok {
		return false
	}
	if startErr := h.startResource(ctx, res, resolved); startErr != nil {
		h.unresolved[name] = startErr
		return false
	}
	return true
}

func refsFor(stack *config.Stack, kind config.ReferenceKind, name string) []config.SecretRef {
	if stack == nil {
		return nil
	}
	var out []config.SecretRef
	for _, ref := range stack.SecretRefs {
		for _, site := range stack.References[ref.Key] {
			if site.Kind == kind && site.Name == name {
				out = append(out, ref)
				break
			}
		}
	}
	return out
}

func serverByName(stack *config.Stack, name string) (config.MCPServer, bool) {
	if stack == nil {
		return config.MCPServer{}, false
	}
	for _, srv := range stack.MCPServers {
		if srv.Name == name {
			return srv, true
		}
	}
	return config.MCPServer{}, false
}

func resourceByName(stack *config.Stack, name string) (config.Resource, bool) {
	if stack == nil {
		return config.Resource{}, false
	}
	for _, res := range stack.Resources {
		if res.Name == name {
			return res, true
		}
	}
	return config.Resource{}, false
}

func serverInDiff(diff *ConfigDiff, name string) bool {
	if diff == nil {
		return false
	}
	for _, srv := range diff.MCPServers.Added {
		if srv.Name == name {
			return true
		}
	}
	for _, srv := range diff.MCPServers.Enabled {
		if srv.Name == name {
			return true
		}
	}
	for _, change := range diff.MCPServers.Modified {
		if change.Name == name {
			return true
		}
	}
	return false
}

func resourceInDiff(diff *ConfigDiff, name string) bool {
	if diff == nil {
		return false
	}
	for _, res := range diff.Resources.Added {
		if res.Name == name {
			return true
		}
	}
	for _, change := range diff.Resources.Modified {
		if change.Name == name {
			return true
		}
	}
	return false
}

func filterServers(in []config.MCPServer, failures map[string]error) []config.MCPServer {
	if len(in) == 0 {
		return in
	}
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
	if len(in) == 0 {
		return in
	}
	out := make([]config.Resource, 0, len(in))
	for _, res := range in {
		if _, failed := failures[res.Name]; failed {
			continue
		}
		out = append(out, res)
	}
	return out
}

func filterServerChanges(in []MCPServerChange, failures map[string]error) []MCPServerChange {
	if len(in) == 0 {
		return in
	}
	out := make([]MCPServerChange, 0, len(in))
	for _, change := range in {
		if _, failed := failures[change.Name]; failed {
			continue
		}
		out = append(out, change)
	}
	return out
}

func filterResourceChanges(in []ResourceChange, failures map[string]error) []ResourceChange {
	if len(in) == 0 {
		return in
	}
	out := make([]ResourceChange, 0, len(in))
	for _, change := range in {
		if _, failed := failures[change.Name]; failed {
			continue
		}
		out = append(out, change)
	}
	return out
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyErrorMap(in map[string]error) map[string]error {
	if in == nil {
		return map[string]error{}
	}
	out := make(map[string]error, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyTimeMap(in map[string]time.Time) map[string]time.Time {
	out := make(map[string]time.Time, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
