package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	vaultpkg "github.com/gridctl/gridctl/pkg/vault"
)

// loadConfig holds options for LoadStack.
type loadConfig struct {
	quiet            bool
	vault            VaultLookup
	vaultSet         VaultSetLookup
	overridePath     string
	noOverride       bool
	overrideRequired bool
}

// LoadOption configures LoadStack behavior.
type LoadOption func(*loadConfig)

// WithQuietLoad suppresses optional variable hints for value-free callers.
func WithQuietLoad() LoadOption {
	return func(c *loadConfig) { c.quiet = true }
}

// WithVault enables ${vault:KEY} resolution during stack loading.
func WithVault(v VaultLookup) LoadOption {
	return func(c *loadConfig) { c.vault = v }
}

// WithVaultSets enables secrets.sets injection during stack loading.
func WithVaultSets(v VaultSetLookup) LoadOption {
	return func(c *loadConfig) { c.vaultSet = v }
}

// WithOverride selects an override file instead of discovering
// stack.override.yaml beside the stack. A missing file is not an error
// unless RequireOverride is also set.
func WithOverride(path string) LoadOption {
	return func(c *loadConfig) { c.overridePath = path }
}

// WithoutOverride suppresses override discovery and merge.
func WithoutOverride() LoadOption {
	return func(c *loadConfig) { c.noOverride = true }
}

// RequireOverride makes a WithOverride path mandatory. An explicit
// --override flag uses this so a missing file fails the load.
func RequireOverride() LoadOption {
	return func(c *loadConfig) { c.overrideRequired = true }
}

// LoadStack reads and parses a stack file.
func LoadStack(path string, opts ...LoadOption) (*Stack, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading stack file: %w", err)
	}
	return LoadStackData(data, path, opts...)
}

// LoadStackData runs the load pipeline on already-read stack bytes. path is
// the file those bytes belong to: extends and relative paths resolve against
// it. The pipeline is decode, extends, expand, defaults, relative paths,
// validate, then optional secret-set injection.
func LoadStackData(data []byte, path string, opts ...LoadOption) (*Stack, error) {
	var cfg loadConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	stack, err := DecodeStack(data)
	if err != nil {
		var ve ValidationErrors
		if errors.As(err, &ve) {
			return nil, err
		}
		return nil, fmt.Errorf("parsing stack YAML: %w", err)
	}

	// Resolve extends chain before variable expansion
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving stack path: %w", err)
	}
	visited := map[string]bool{absPath: true}
	if err := resolveExtends(stack, absPath, visited, 0); err != nil {
		return nil, err
	}

	// Merge the override after extends and before expansion so override
	// env values may carry ${var:} references and the merged result is
	// defaulted and validated exactly once.
	if err := mergeLoadOverride(stack, absPath, &cfg); err != nil {
		return nil, err
	}

	// Expand variable references in string values
	unresolved, emptyVars, resolutionProblems := expandStackVarsResolved(stack, newReferenceResolver(cfg.vault))
	if len(resolutionProblems) > 0 {
		return nil, fmt.Errorf("resolving stack variable: %w", resolutionProblems[0])
	}

	// A key consumed only by disabled servers or resources does not block
	// deploy. A key with any enabled consumer still refuses.
	unresolved = dropDisabledOnlyUnresolved(stack, unresolved)

	// Fail on unresolved variable references only if a store was provided.
	if cfg.vault != nil && len(unresolved) > 0 {
		msg := fmt.Sprintf("missing variable(s): %s", strings.Join(unresolved, ", "))
		msg += "\n  To fix: gridctl var set <KEY>"
		msg += "\n  Diagnose: gridctl var explain <KEY>"
		return nil, fmt.Errorf("%s", msg)
	}

	// Hint about empty env vars that could use the variable store
	if cfg.vault == nil && !cfg.quiet {
		for _, v := range emptyVars {
			slog.Info("hint: "+v+" resolved to empty — use 'gridctl var set "+v+"' to store it securely", "var", v)
		}
	}

	// Apply defaults
	stack.SetDefaults()

	// Resolve relative paths based on stack file location
	basePath := filepath.Dir(path)
	resolveRelativePaths(stack, basePath)

	// Validate the stack
	if err := Validate(stack); err != nil {
		return nil, err
	}

	// Inject variable set secrets into container env
	if stack.Secrets != nil && len(stack.Secrets.Sets) > 0 && cfg.vaultSet != nil {
		injectSetSecrets(stack, cfg.vaultSet)
	}

	return stack, nil
}

// dropDisabledOnlyUnresolved removes keys whose every consumer is a
// disabled server or resource. A key with no consumers, or with any
// enabled or non-workload consumer, still blocks deploy. Exempted keys
// are logged with the consumer names.
func dropDisabledOnlyUnresolved(stack *Stack, unresolved []string) []string {
	if stack == nil || len(unresolved) == 0 {
		return unresolved
	}
	blocking := make([]string, 0, len(unresolved))
	for _, key := range unresolved {
		consumers := stack.References[key]
		if !allConsumersDisabled(stack, consumers) {
			blocking = append(blocking, key)
			continue
		}
		names := make([]string, 0, len(consumers))
		for _, c := range consumers {
			names = append(names, string(c.Kind)+":"+c.Name)
		}
		slog.Info("skipping unresolved variable consumed only by disabled workloads",
			"var", key, "consumers", strings.Join(names, ", "))
	}
	return blocking
}

func allConsumersDisabled(stack *Stack, consumers []Consumer) bool {
	if len(consumers) == 0 {
		return false
	}
	for _, c := range consumers {
		switch c.Kind {
		case RefKindMCPServer:
			idx := indexMCPServer(stack, c.Name)
			if idx < 0 || stack.MCPServers[idx].IsEnabled() {
				return false
			}
		case RefKindResource:
			if res := resourceByName(stack, c.Name); res == nil || res.IsEnabled() {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func resourceByName(stack *Stack, name string) *Resource {
	if stack == nil {
		return nil
	}
	for i := range stack.Resources {
		if stack.Resources[i].Name == name {
			return &stack.Resources[i]
		}
	}
	return nil
}

// injectSetSecrets resolves secrets from variable sets and injects them into
// container env. Explicit env values in YAML take precedence over set-injected
// values.
//
// Each set entry decides its own reach: an unscoped entry fans out to every
// server and resource (historic behavior), while a scoped entry reaches only
// the workloads it names. Sets are applied in declaration order and an earlier
// set wins a key collision, matching the "first writer wins" rule that already
// gives explicit YAML env precedence.
func injectSetSecrets(s *Stack, vault VaultSetLookup) {
	// Resolve each set once. A name can still repeat here when every
	// occurrence is bare (validation allows that for back-compat), which is
	// idempotent: the same members, the same fan-out.
	secretsFor := make(map[string]map[string]string, len(s.Secrets.Sets))
	for _, ref := range s.Secrets.Sets {
		if _, done := secretsFor[ref.Name]; done {
			continue
		}
		members := make(map[string]string)
		for _, sec := range vault.GetSetSecrets(ref.Name) {
			if vaultpkg.IsInternalCredential(sec.Key) {
				continue
			}
			members[sec.Key] = sec.Value
		}
		secretsFor[ref.Name] = members
	}

	inject := func(env map[string]string, values map[string]string) {
		for k, v := range values {
			if _, exists := env[k]; !exists {
				env[k] = v
			}
		}
	}

	for i := range s.MCPServers {
		srv := &s.MCPServers[i]
		if !srv.IsEnabled() {
			continue
		}
		for _, ref := range s.Secrets.Sets {
			values := secretsFor[ref.Name]
			if len(values) == 0 || !ref.InjectsIntoServer(srv.Name) {
				continue
			}
			if srv.Env == nil {
				srv.Env = make(map[string]string)
			}
			inject(srv.Env, values)
		}
	}

	for i := range s.Resources {
		res := &s.Resources[i]
		if !res.IsEnabled() {
			continue
		}
		for _, ref := range s.Secrets.Sets {
			values := secretsFor[ref.Name]
			if len(values) == 0 || !ref.InjectsIntoResource(res.Name) {
				continue
			}
			if res.Env == nil {
				res.Env = make(map[string]string)
			}
			inject(res.Env, values)
		}
	}
}

// expandStackVars expands variable references in all stack string fields using
// the unified ExpandString grammar, and as it goes records every variable-store
// reference into s.References (the usage index). Returns unresolved vault
// references and empty env vars.
//
// Every field expansion flows through the single expandField helper, which both
// expands and indexes. This is deliberate: a reference can never be expanded
// without also being indexed, so the usage index cannot drift from what
// expansion recognizes (see references_test.go's parity test). When adding a
// newly expandable field, route it through expandField and it is indexed for
// free.
func expandStackVars(s *Stack, resolve Resolver) (unresolvedVault []string, emptyEnvVars []string) {
	resolved, empty, _ := expandStackVarsResolved(s, func(name string, _ bool) ResolutionResult {
		value, ok := resolve(name)
		if !ok {
			return ResolutionResult{Verdict: ResolutionUnset}
		}
		return ResolutionResult{Value: value, Verdict: ResolutionEnvFallback}
	})
	return resolved, empty
}

func expandStackVarsResolved(s *Stack, resolve referenceResolver) (unresolvedVault []string, emptyEnvVars []string, resolutionProblems []error) {
	index := ReferenceIndex{}
	var secretRefs []SecretRef
	seenSecret := map[string]bool{}
	if s != nil {
		s.emptySecretSites = nil
	}

	// expandField expands one field's value and records every ${var:KEY}
	// and ${secret:} reference it contains against the consumer site c.
	expandField := func(c Consumer, val string) string {
		result, denied := expandStringRefs(val, resolve)
		for _, key := range result.storeRefs {
			index.add(key, c)
		}
		for _, key := range result.secretRefs {
			index.add(key, c)
			if !seenSecret[key] {
				seenSecret[key] = true
				provider, path, _ := ParseSecretRefKey(key)
				secretRefs = append(secretRefs, SecretRef{Provider: provider, Path: path, Key: key})
			}
		}
		if result.emptySecretPath {
			s.emptySecretSites = append(s.emptySecretSites, c)
		}
		unresolvedVault = append(unresolvedVault, result.unresolvedVault...)
		emptyEnvVars = append(emptyEnvVars, result.emptyEnvVars...)
		resolutionProblems = append(resolutionProblems, denied...)
		return result.expanded
	}

	walkExpandedStrings(s, expandField)

	s.References = index
	s.SecretRefs = secretRefs
	s.UnresolvedRefs = dedupeStrings(unresolvedVault)
	return unresolvedVault, emptyEnvVars, resolutionProblems
}

// SubstituteSecretRefs rewrites ${secret:} tokens in every field
// expandStackVarsResolved covers. Missing keys stay literal. The walk matches
// the loader so a newly expandable field is substituted when it is indexed.
func SubstituteSecretRefs(s *Stack, values map[string]string) {
	if s == nil || len(values) == 0 {
		return
	}
	walkExpandedStrings(s, func(_ Consumer, val string) string {
		return replaceSecretRefs(val, values)
	})
}

// walkExpandedStrings visits every expandable string field. fn returns the
// replacement. CredentialRef is intentionally omitted, matching expansion.
func walkExpandedStrings(s *Stack, fn func(Consumer, string) string) {
	s.Name = fn(Consumer{Kind: RefKindStack, Field: "name"}, s.Name)

	if s.Gateway != nil {
		for i := range s.Gateway.AllowedOrigins {
			s.Gateway.AllowedOrigins[i] = fn(
				Consumer{Kind: RefKindGateway, Field: fmt.Sprintf("allowed_origins[%d]", i)},
				s.Gateway.AllowedOrigins[i])
		}
		s.Gateway.Bind = fn(Consumer{Kind: RefKindGateway, Field: "bind"}, s.Gateway.Bind)
		for i := range s.Gateway.AllowedHosts {
			s.Gateway.AllowedHosts[i] = fn(
				Consumer{Kind: RefKindGateway, Field: fmt.Sprintf("allowed_hosts[%d]", i)},
				s.Gateway.AllowedHosts[i])
		}
		if s.Gateway.Auth != nil {
			s.Gateway.Auth.Token = fn(Consumer{Kind: RefKindGateway, Field: "auth.token"}, s.Gateway.Auth.Token)
		}
	}

	s.Network.Name = fn(Consumer{Kind: RefKindNetwork, Field: "name"}, s.Network.Name)
	for i := range s.Networks {
		s.Networks[i].Name = fn(
			Consumer{Kind: RefKindNetwork, Name: s.Networks[i].Name, Field: "name"}, s.Networks[i].Name)
	}

	for i := range s.MCPServers {
		srv := &s.MCPServers[i]
		site := func(field string) Consumer {
			return Consumer{Kind: RefKindMCPServer, Name: srv.Name, Field: field}
		}
		srv.Name = fn(site("name"), srv.Name)
		srv.Image = fn(site("image"), srv.Image)
		srv.URL = fn(site("url"), srv.URL)
		srv.Network = fn(site("network"), srv.Network)
		for j := range srv.Command {
			srv.Command[j] = fn(site(fmt.Sprintf("command[%d]", j)), srv.Command[j])
		}
		if srv.Source != nil {
			srv.Source.URL = fn(site("source.url"), srv.Source.URL)
			srv.Source.Path = fn(site("source.path"), srv.Source.Path)
			srv.Source.ProjectPath = fn(site("source.project_path"), srv.Source.ProjectPath)
			srv.Source.Ref = fn(site("source.ref"), srv.Source.Ref)
			srv.Source.Dockerfile = fn(site("source.dockerfile"), srv.Source.Dockerfile)
			srv.Source.Package = fn(site("source.package"), srv.Source.Package)
			srv.Source.Python = fn(site("source.python"), srv.Source.Python)
			for j := range srv.Source.Extras {
				srv.Source.Extras[j] = fn(site(fmt.Sprintf("source.extras[%d]", j)), srv.Source.Extras[j])
			}
			for j := range srv.Source.With {
				srv.Source.With[j] = fn(site(fmt.Sprintf("source.with[%d]", j)), srv.Source.With[j])
			}
			for j := range srv.Source.Packages {
				srv.Source.Packages[j] = fn(site(fmt.Sprintf("source.packages[%d]", j)), srv.Source.Packages[j])
			}
			if srv.Source.Auth != nil {
				srv.Source.Auth.SSHKeyPath = fn(site("source.auth.ssh_key_path"), srv.Source.Auth.SSHKeyPath)
				srv.Source.Auth.SSHUser = fn(site("source.auth.ssh_user"), srv.Source.Auth.SSHUser)
			}
		}
		for k, v := range srv.Env {
			srv.Env[k] = fn(site("env."+k), v)
		}
		for k, v := range srv.BuildArgs {
			srv.BuildArgs[k] = fn(site("build_args."+k), v)
		}
		for j := range srv.Volumes {
			srv.Volumes[j] = fn(site(fmt.Sprintf("volumes[%d]", j)), srv.Volumes[j])
		}
		for j := range srv.Configs {
			srv.Configs[j].Content = fn(site(fmt.Sprintf("configs[%d].content", j)), srv.Configs[j].Content)
			srv.Configs[j].File = fn(site(fmt.Sprintf("configs[%d].file", j)), srv.Configs[j].File)
		}
		if srv.SSH != nil {
			srv.SSH.Host = fn(site("ssh.host"), srv.SSH.Host)
			srv.SSH.User = fn(site("ssh.user"), srv.SSH.User)
			srv.SSH.IdentityFile = fn(site("ssh.identityFile"), srv.SSH.IdentityFile)
			srv.SSH.KnownHostsFile = fn(site("ssh.knownHostsFile"), srv.SSH.KnownHostsFile)
			srv.SSH.JumpHost = fn(site("ssh.jumpHost"), srv.SSH.JumpHost)
		}
		if srv.A2A != nil {
			srv.A2A.Card = fn(site("a2a.card"), srv.A2A.Card)
			srv.A2A.Endpoint = fn(site("a2a.endpoint"), srv.A2A.Endpoint)
			if srv.A2A.Auth != nil {
				srv.A2A.Auth.Token = fn(site("a2a.auth.token"), srv.A2A.Auth.Token)
			}
		}
		if srv.OpenAPI != nil {
			srv.OpenAPI.Spec = fn(site("openapi.spec"), srv.OpenAPI.Spec)
			srv.OpenAPI.BaseURL = fn(site("openapi.baseUrl"), srv.OpenAPI.BaseURL)
		}
		if srv.Auth != nil {
			srv.Auth.Token = fn(site("auth.token"), srv.Auth.Token)
			srv.Auth.Value = fn(site("auth.value"), srv.Auth.Value)
			srv.Auth.ClientID = fn(site("auth.client_id"), srv.Auth.ClientID)
			srv.Auth.ClientSecret = fn(site("auth.client_secret"), srv.Auth.ClientSecret)
		}
		for name, value := range srv.Headers {
			srv.Headers[name] = fn(site("headers."+name), value)
		}
	}

	for i := range s.Resources {
		res := &s.Resources[i]
		site := func(field string) Consumer {
			return Consumer{Kind: RefKindResource, Name: res.Name, Field: field}
		}
		res.Name = fn(site("name"), res.Name)
		res.Image = fn(site("image"), res.Image)
		res.Network = fn(site("network"), res.Network)
		for k, v := range res.Env {
			res.Env[k] = fn(site("env."+k), v)
		}
		for j := range res.Volumes {
			res.Volumes[j] = fn(site(fmt.Sprintf("volumes[%d]", j)), res.Volumes[j])
		}
	}
}

// dedupeStrings returns the input with duplicates removed, preserving first-seen
// order so callers get a stable list.
func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// resolveRelativePaths resolves local source paths relative to the stack file.
func resolveRelativePaths(s *Stack, basePath string) {
	for i := range s.MCPServers {
		if s.MCPServers[i].Source != nil && s.MCPServers[i].Source.Type == "local" {
			if !filepath.IsAbs(s.MCPServers[i].Source.Path) {
				s.MCPServers[i].Source.Path = filepath.Join(basePath, s.MCPServers[i].Source.Path)
			}
		}

		// Resolve SSH identity file paths
		if s.MCPServers[i].SSH != nil && s.MCPServers[i].SSH.IdentityFile != "" {
			s.MCPServers[i].SSH.IdentityFile = expandTildeAndResolvePath(s.MCPServers[i].SSH.IdentityFile, basePath)
		}
		if s.MCPServers[i].SSH != nil && s.MCPServers[i].SSH.KnownHostsFile != "" {
			s.MCPServers[i].SSH.KnownHostsFile = expandTildeAndResolvePath(s.MCPServers[i].SSH.KnownHostsFile, basePath)
		}

		// Resolve source.auth.ssh_key_path (mirrors SSH.IdentityFile handling).
		if s.MCPServers[i].Source != nil && s.MCPServers[i].Source.Auth != nil && s.MCPServers[i].Source.Auth.SSHKeyPath != "" {
			s.MCPServers[i].Source.Auth.SSHKeyPath = expandTildeAndResolvePath(s.MCPServers[i].Source.Auth.SSHKeyPath, basePath)
		}

		// Resolve OpenAPI spec paths (if not a URL)
		if s.MCPServers[i].OpenAPI != nil && s.MCPServers[i].OpenAPI.Spec != "" {
			if !isURL(s.MCPServers[i].OpenAPI.Spec) {
				s.MCPServers[i].OpenAPI.Spec = expandTildeAndResolvePath(s.MCPServers[i].OpenAPI.Spec, basePath)
			}
		}

		for j := range s.MCPServers[i].Volumes {
			s.MCPServers[i].Volumes[j] = resolveVolumeHost(s.MCPServers[i].Volumes[j], basePath)
		}
		for j := range s.MCPServers[i].Configs {
			file := s.MCPServers[i].Configs[j].File
			if file == "" || filepath.IsAbs(file) {
				continue
			}
			s.MCPServers[i].Configs[j].File = expandTildeAndResolvePath(file, basePath)
		}
	}
	for i := range s.Resources {
		for j := range s.Resources[i].Volumes {
			s.Resources[i].Volumes[j] = resolveVolumeHost(s.Resources[i].Volumes[j], basePath)
		}
	}
}

// resolveVolumeHost rewrites a host:container[:mode] source when the host
// part is stack-relative. Named volumes and absolute paths are unchanged.
// Only the first colon separates the host part, so a Windows drive letter
// is not treated as a relative source.
func resolveVolumeHost(volume, basePath string) string {
	host, rest, ok := splitVolume(volume)
	if !ok || !stackRelativeHost(host) {
		return volume
	}
	return expandTildeAndResolvePath(host, basePath) + rest
}

func splitVolume(volume string) (host, rest string, ok bool) {
	idx := strings.Index(volume, ":")
	if idx <= 0 {
		return "", "", false
	}
	return volume[:idx], volume[idx:], true
}

func stackRelativeHost(host string) bool {
	return strings.HasPrefix(host, "./") || strings.HasPrefix(host, "../") || strings.HasPrefix(host, "~")
}

// expandTildeAndResolvePath expands ~ to home directory and resolves relative paths.
func expandTildeAndResolvePath(path, basePath string) string {
	// Expand ~ to home directory. Deliberately the real OS home, not
	// state.Home(): the tilde appears in user-authored stack.yaml paths,
	// where ~ means the user's actual home regardless of GRIDCTL_HOME
	// (see pkg/state/homeguard_test.go).
	if len(path) > 0 && path[0] == '~' {
		if home, err := os.UserHomeDir(); err == nil {
			if len(path) == 1 {
				path = home
			} else if path[1] == '/' || path[1] == filepath.Separator {
				path = filepath.Join(home, path[2:])
			}
		}
	}

	// Resolve relative paths
	if !filepath.IsAbs(path) {
		path = filepath.Join(basePath, path)
	}

	return path
}

// isURL checks if a string looks like a URL (http:// or https://).
func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

const maxExtendsDepth = 10

// annotateInheritedErrors names the parent file on validation findings that
// were decoded from an extends parent. The line number is the parent's line;
// without the file name, validate prints it under the child path.
func annotateInheritedErrors(err error, parentPath string) error {
	var ve ValidationErrors
	if !errors.As(err, &ve) {
		return err
	}
	label := filepath.Base(parentPath)
	if label == "" || label == "." {
		label = "parent"
	}
	out := make(ValidationErrors, len(ve))
	for i, item := range ve {
		field := item.Field
		if field == "" {
			field = "yaml"
		}
		item.Field = "extends[" + label + "]." + field
		out[i] = item
	}
	return out
}

// resolveExtends loads the parent stack referenced by child.Extends, merges it into
// child, and clears child.Extends. Called recursively to support multi-level inheritance.
// visited tracks absolute paths already in the chain to detect cycles.
func resolveExtends(child *Stack, childAbsPath string, visited map[string]bool, depth int) error {
	if child.Extends == "" {
		return nil
	}
	if depth >= maxExtendsDepth {
		return fmt.Errorf("extends: maximum inheritance depth (%d) exceeded", maxExtendsDepth)
	}

	// Resolve parent path relative to the child file's directory
	parentPath := child.Extends
	if !filepath.IsAbs(parentPath) {
		parentPath = filepath.Join(filepath.Dir(childAbsPath), parentPath)
	}
	absParentPath, err := filepath.Abs(parentPath)
	if err != nil {
		return fmt.Errorf("extends: resolving path %q: %w", child.Extends, err)
	}

	// Cycle detection
	if visited[absParentPath] {
		return fmt.Errorf("extends: circular dependency detected: %s → %s", childAbsPath, absParentPath)
	}
	visited[absParentPath] = true

	// Read and unmarshal parent
	data, err := os.ReadFile(absParentPath)
	if err != nil {
		return fmt.Errorf("extends: reading parent stack: %w", err)
	}

	parent, err := DecodeStack(data)
	if err != nil {
		var ve ValidationErrors
		if errors.As(err, &ve) {
			return annotateInheritedErrors(err, absParentPath)
		}
		return fmt.Errorf("extends: parsing parent stack: %w", err)
	}

	// Recurse before merging so the full ancestor chain is resolved first
	if err := resolveExtends(parent, absParentPath, visited, depth+1); err != nil {
		return err
	}
	if err := checkDeclarationConflicts(child.Variables, parent.Variables); err != nil {
		return fmt.Errorf("extends: %w", err)
	}

	// Resolve parent's relative paths against parent's directory before merging into child.
	// Without this, inherited paths would be re-resolved against the child's directory.
	resolveRelativePaths(parent, filepath.Dir(absParentPath))

	mergeStacks(child, parent)
	child.Extends = ""
	return nil
}

// mergeStacks merges parent into child using child-wins semantics:
//   - MCPServers and Resources: child entries kept as-is; parent-only entries appended
//   - Gateway, Logging, Secrets sets, Network/Networks: inherited from parent when child omits them
//   - secrets.providers: parent entries whose name the child does not declare are appended,
//     whether or not the child has a secrets block. A same-named child provider wins.
//     secrets.sets stays whole-block and is inherited only when the child omits secrets.
//
// Everything absent from this switchboard is deliberately NOT inherited
// across `extends`: clients, groups, limits, policy, skills, link, telemetry,
// experimental, and model_preferences all stay child-only (a policy
// block silently inherited from a parent stack is a policy the operator
// never sees). A new block needs an explicit decision here either way.
func mergeStacks(child, parent *Stack) {
	mergeVariableDeclarations(child, parent)
	// MCPServers: child wins on name collision; parent-only servers appended
	if len(parent.MCPServers) > 0 {
		childNames := make(map[string]bool, len(child.MCPServers))
		for _, s := range child.MCPServers {
			childNames[s.Name] = true
		}
		for _, s := range parent.MCPServers {
			if !childNames[s.Name] {
				child.MCPServers = append(child.MCPServers, s)
			}
		}
	}

	// Resources: same merge-by-name algorithm
	if len(parent.Resources) > 0 {
		childResourceNames := make(map[string]bool, len(child.Resources))
		for _, r := range child.Resources {
			childResourceNames[r.Name] = true
		}
		for _, r := range parent.Resources {
			if !childResourceNames[r.Name] {
				child.Resources = append(child.Resources, r)
			}
		}
	}

	// Top-level blocks: inherit from parent when child has no value
	if child.Gateway == nil {
		child.Gateway = parent.Gateway
	}
	if child.Logging == nil {
		child.Logging = parent.Logging
	}
	if child.Secrets == nil {
		child.Secrets = parent.Secrets
	} else if parent.Secrets != nil {
		declared := make(map[string]bool, len(child.Secrets.Providers))
		for _, p := range child.Secrets.Providers {
			declared[p.Name] = true
		}
		for _, p := range parent.Secrets.Providers {
			if p.Name == "" || declared[p.Name] {
				continue
			}
			child.Secrets.Providers = append(child.Secrets.Providers, p)
			declared[p.Name] = true
		}
	}
	if child.Network.Name == "" && len(child.Networks) == 0 {
		child.Network = parent.Network
		child.Networks = parent.Networks
	}
}

func mergeVariableDeclarations(child, parent *Stack) {
	if len(parent.Variables) == 0 {
		return
	}
	if child.Variables == nil {
		child.Variables = make(map[string]VariableDeclaration, len(parent.Variables))
	}
	for key, p := range parent.Variables {
		c, ok := child.Variables[key]
		if !ok {
			child.Variables[key] = p
			continue
		}
		if c.Required == nil {
			c.Required = p.Required
		} else if p.IsRequired() && !c.IsRequired() {
			v := true
			c.Required = &v
		}
		if c.Secret == nil {
			c.Secret = p.Secret
		}
		if c.Type == "" {
			c.Type = p.Type
		}
		if c.Description == "" {
			c.Description = p.Description
		}
		if c.Docs == "" {
			c.Docs = p.Docs
		}
		child.Variables[key] = c
	}
}
