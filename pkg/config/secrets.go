package config

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	secretProviderTypeExec        = "exec"
	secretProviderTypeOnePassword = "onepassword"
	defaultSecretTimeout          = 10 * time.Second
	minSecretTimeout              = time.Second
	maxSecretTimeout              = 5 * time.Minute
)

var secretProviderNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// MarshalJSON retains explicit empty scopes instead of broadening delivery.
func (r SecretSetRef) MarshalJSON() ([]byte, error) {
	out := map[string]any{"name": r.Name}
	if len(r.Servers) > 0 {
		out["servers"] = r.Servers
	}
	if len(r.Resources) > 0 {
		out["resources"] = r.Resources
	}
	if r.Scoped() && len(r.Servers) == 0 && len(r.Resources) == 0 {
		out["servers"] = []string{}
	}
	return json.Marshal(out)
}

// UnmarshalJSON preserves explicit scope presence in exported JSON.
func (r *SecretSetRef) UnmarshalJSON(data []byte) error {
	type rawSecretSetRef SecretSetRef
	var raw rawSecretSetRef
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	_, servers := fields["servers"]
	_, resources := fields["resources"]
	raw.scopeDeclared = servers || resources
	*r = SecretSetRef(raw)
	return nil
}

// SecretSetRef is one entry in the `secrets.sets` block: a variable set whose
// members are injected into container environments at load time (see
// injectSetSecrets).
//
// An entry is either a bare set name or a mapping that narrows which workloads
// receive the set:
//
//	secrets:
//	  sets:
//	    - shared              # unscoped: injected into every server and resource
//	    - name: github-creds  # scoped: only the listed workloads receive it
//	      servers: [github]
//	    - name: db
//	      resources: [postgres]
//
// Scoping is opt-in and per entry (Article IX). An entry with neither servers
// nor resources keeps the historic fan-out, so a stack written before scoping
// existed behaves identically. Naming either list makes the entry scoped, and
// a scoped entry reaches only what it names: `servers: [github]` injects into
// the github server and into no resources at all. That is the least-privilege
// reading, and it keeps one rule ("scoped entries reach exactly what they
// name") rather than two axes with different defaults.
type SecretSetRef struct {
	Name      string   `yaml:"name" json:"name"`
	Servers   []string `yaml:"servers,omitempty" json:"servers,omitempty"`
	Resources []string `yaml:"resources,omitempty" json:"resources,omitempty"`

	// scopeDeclared records that the YAML named `servers` or `resources`, even
	// with an empty list. Without it, `servers: []` would be indistinguishable
	// from an absent key and would fan out to everything, which is the exact
	// opposite of what someone writing an empty scope means. Set only by
	// UnmarshalYAML; entries built in Go are scoped when either list is
	// non-empty.
	scopeDeclared bool
}

// secretSetRefKeys is the set of keys the mapping form accepts. A typo like
// `server:` (singular) would otherwise be silently dropped by the YAML decoder
// and leave the entry unscoped, quietly fanning a credential out to every
// workload instead of the one the author named.
var secretSetRefKeys = map[string]bool{
	"name":      true,
	"servers":   true,
	"resources": true,
}

// UnmarshalYAML accepts the scalar shorthand ("- shared") or the mapping form.
// Any other node kind is rejected so a stray nested sequence fails loudly
// instead of decoding to an empty entry. Mirrors LinkEntry, with the addition
// of unknown-key rejection because a dropped key here fails open.
func (r *SecretSetRef) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		return value.Decode(&r.Name)
	case yaml.MappingNode:
		// Mapping content alternates key, value.
		for i := 0; i+1 < len(value.Content); i += 2 {
			key := value.Content[i].Value
			if !secretSetRefKeys[key] {
				return fmt.Errorf(
					"secrets.sets entry has unknown field %q (line %d); expected name, servers, or resources",
					key, value.Content[i].Line)
			}
			if key == "servers" || key == "resources" {
				r.scopeDeclared = true
			}
		}
		type rawSecretSetRef SecretSetRef // shed the method to avoid recursion
		raw := (*rawSecretSetRef)(r)
		declared := r.scopeDeclared
		if err := value.Decode(raw); err != nil {
			return err
		}
		r.scopeDeclared = declared
		return nil
	default:
		return fmt.Errorf("secrets.sets entry must be a set name or a mapping (line %d)", value.Line)
	}
}

// MarshalYAML emits the scalar shorthand for unscoped entries so a stack that
// was written as `- shared` survives a load/save cycle unchanged. Scoped
// entries marshal as mappings, keeping an explicitly empty scope explicit.
func (r SecretSetRef) MarshalYAML() (any, error) {
	if r.IsShorthand() {
		return r.Name, nil
	}
	out := map[string]any{"name": r.Name}
	if len(r.Servers) > 0 {
		out["servers"] = r.Servers
	}
	if len(r.Resources) > 0 {
		out["resources"] = r.Resources
	}
	if len(r.Servers) == 0 && len(r.Resources) == 0 {
		// A scope that names nothing must survive the round trip as a scope.
		// Emitting neither key would read back as a fan-out entry, quietly
		// widening access on the next load.
		out["servers"] = []string{}
	}
	return out, nil
}

// IsShorthand reports whether the entry carries nothing beyond the set name,
// so YAML emitters can round-trip it back to the scalar form.
func (r SecretSetRef) IsShorthand() bool {
	return !r.Scoped()
}

// Scoped reports whether the entry narrows injection to named workloads.
// Unscoped entries fan out to every server and resource. An entry that
// declared an empty scope is scoped (to nothing), not unscoped.
func (r SecretSetRef) Scoped() bool {
	return r.scopeDeclared || len(r.Servers) > 0 || len(r.Resources) > 0
}

// InjectsIntoServer reports whether this entry's secrets reach the named MCP
// server. Unscoped entries reach every server.
func (r SecretSetRef) InjectsIntoServer(name string) bool {
	if !r.Scoped() {
		return true
	}
	return containsName(r.Servers, name)
}

// InjectsIntoResource reports whether this entry's secrets reach the named
// resource. Unscoped entries reach every resource.
func (r SecretSetRef) InjectsIntoResource(name string) bool {
	if !r.Scoped() {
		return true
	}
	return containsName(r.Resources, name)
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// validateSecrets checks the optional `secrets:` block and secret references.
// Set checks: every entry must name a set, a set may be listed once, and a
// scoped entry may only name servers and resources the stack declares.
// Provider checks never run a subprocess. The declared-provider check is
// skipped while Extends is set, because ValidateStackFile has not merged the
// parent yet. LoadStack clears Extends after merging, so the check runs there.
func validateSecrets(s *Stack) ValidationErrors {
	var errs ValidationErrors
	errs = append(errs, validateSecretProviders(s)...)
	errs = append(errs, validateSecretReferenceUses(s)...)
	if s.Secrets == nil {
		return errs
	}

	serverNames := make(map[string]bool, len(s.MCPServers))
	for _, srv := range s.MCPServers {
		serverNames[srv.Name] = true
	}
	resourceNames := make(map[string]bool, len(s.Resources))
	for _, res := range s.Resources {
		resourceNames[res.Name] = true
	}

	// A set name may repeat only while every occurrence is bare. Repeated bare
	// names predate scoping and stay valid (Article IX): injection is
	// idempotent, so listing a set twice is a harmless no-op. Once any
	// occurrence is scoped the repetition stops being harmless, because the
	// entries' reaches union: a bare `- dev` alongside `- name: dev, servers:
	// [a]` fans the set out to every workload while reading as if it were
	// confined to one, which defeats the point of scoping it.
	firstScoped := make(map[string]int, len(s.Secrets.Sets))
	firstAny := make(map[string]int, len(s.Secrets.Sets))
	for i, ref := range s.Secrets.Sets {
		if ref.Name == "" {
			continue
		}
		if prev, seen := firstAny[ref.Name]; seen {
			if ref.Scoped() || s.Secrets.Sets[prev].Scoped() {
				at := prev
				if _, ok := firstScoped[ref.Name]; ok {
					at = firstScoped[ref.Name]
				}
				errs = append(errs, ValidationError{
					fmt.Sprintf("secrets.sets[%d]", i),
					fmt.Sprintf("set %q is already listed at secrets.sets[%d]; a scoped set may be listed once, since repeated entries inject the union of their scopes", ref.Name, at),
				})
			}
		} else {
			firstAny[ref.Name] = i
		}
		if ref.Scoped() {
			if _, ok := firstScoped[ref.Name]; !ok {
				firstScoped[ref.Name] = i
			}
		}
	}

	for i, ref := range s.Secrets.Sets {
		prefix := fmt.Sprintf("secrets.sets[%d]", i)

		// Only the scoped form is validated for shape. A bare name that is
		// empty was accepted before scoping existed and stays accepted
		// (Article IX): an unknown set resolves to no members.
		if !ref.Scoped() {
			continue
		}

		if ref.Name == "" {
			errs = append(errs, ValidationError{prefix, "set name is required"})
			continue
		}

		for j, name := range ref.Servers {
			if !serverNames[name] {
				errs = append(errs, ValidationError{
					fmt.Sprintf("%s.servers[%d]", prefix, j),
					fmt.Sprintf("references unknown MCP server '%s'", name),
				})
			}
		}
		for j, name := range ref.Resources {
			if !resourceNames[name] {
				errs = append(errs, ValidationError{
					fmt.Sprintf("%s.resources[%d]", prefix, j),
					fmt.Sprintf("references unknown resource '%s'", name),
				})
			}
		}

		// A scope that names nothing injects nowhere. That is almost always an
		// unfinished edit, and it fails the same way a typo does: the workload
		// starts without the credential and reports an opaque auth error later.
		if ref.Scoped() && len(ref.Servers) == 0 && len(ref.Resources) == 0 {
			errs = append(errs, ValidationError{
				prefix,
				fmt.Sprintf("set %q is scoped to no servers or resources, so it injects nowhere; remove the empty scope to inject everywhere", ref.Name),
			})
		}
	}
	return errs
}

func validateSecretProviders(s *Stack) ValidationErrors {
	if s == nil || s.Secrets == nil || len(s.Secrets.Providers) == 0 {
		return nil
	}
	var errs ValidationErrors
	seen := make(map[string]int, len(s.Secrets.Providers))
	for i, p := range s.Secrets.Providers {
		prefix := fmt.Sprintf("secrets.providers[%d]", i)
		if !secretProviderNamePattern.MatchString(p.Name) {
			errs = append(errs, ValidationError{prefix + ".name", "name must match ^[a-z][a-z0-9-]*$"})
		} else if prev, ok := seen[p.Name]; ok {
			errs = append(errs, ValidationError{
				prefix + ".name",
				fmt.Sprintf("provider name %q is already declared at secrets.providers[%d]", p.Name, prev),
			})
		} else {
			seen[p.Name] = i
		}
		switch p.Type {
		case secretProviderTypeExec:
			if len(p.Command) == 0 {
				errs = append(errs, ValidationError{prefix + ".command", "command is required"})
			} else if !commandHasPathPlaceholder(p.Command) {
				errs = append(errs, ValidationError{prefix + ".command", "command must contain {{path}}"})
			}
			if p.TokenFile != "" {
				errs = append(errs, ValidationError{prefix + ".token_file", "token_file is not valid for type exec"})
			}
		case secretProviderTypeOnePassword:
			if len(p.Command) > 0 {
				errs = append(errs, ValidationError{prefix + ".command", "command is not valid for type onepassword"})
			}
			if p.TokenFile != "" && !validTokenFilePath(p.TokenFile) {
				errs = append(errs, ValidationError{prefix + ".token_file", "token_file must be absolute or start with ~/"})
			}
		default:
			errs = append(errs, ValidationError{prefix + ".type", "type must be exec or onepassword"})
		}
		if p.Timeout != "" {
			d, err := time.ParseDuration(p.Timeout)
			if err != nil || d < minSecretTimeout || d > maxSecretTimeout {
				errs = append(errs, ValidationError{prefix + ".timeout", "timeout must be a duration between 1s and 5m"})
			}
		}
	}
	return errs
}

func commandHasPathPlaceholder(command []string) bool {
	for _, el := range command {
		if strings.Contains(el, "{{path}}") {
			return true
		}
	}
	return false
}

func validTokenFilePath(path string) bool {
	if strings.HasPrefix(path, "~/") {
		return true
	}
	return filepath.IsAbs(path)
}

// ProviderTimeout returns the configured timeout or the 10s default.
func (p SecretProvider) ProviderTimeout() (time.Duration, error) {
	if p.Timeout == "" {
		return defaultSecretTimeout, nil
	}
	d, err := time.ParseDuration(p.Timeout)
	if err != nil {
		return 0, err
	}
	if d < minSecretTimeout || d > maxSecretTimeout {
		return 0, fmt.Errorf("timeout must be a duration between 1s and 5m")
	}
	return d, nil
}

func validateSecretReferenceUses(s *Stack) ValidationErrors {
	if s == nil {
		return nil
	}
	var errs ValidationErrors
	for _, c := range s.emptySecretSites {
		errs = append(errs, ValidationError{
			secretConsumerField(s, c),
			"secret reference path is empty",
		})
	}
	if len(s.SecretRefs) == 0 {
		return errs
	}
	declared := map[string]bool{}
	if s.Secrets != nil {
		for _, p := range s.Secrets.Providers {
			declared[p.Name] = true
		}
	}
	for _, ref := range s.SecretRefs {
		if strings.Contains(ref.Path, ":-") || strings.Contains(ref.Path, ":+") {
			for _, c := range s.References[ref.Key] {
				errs = append(errs, ValidationError{
					secretConsumerField(s, c),
					"secret references do not take default or replacement operands; supply the fallback outside the stack",
				})
			}
			if len(s.References[ref.Key]) == 0 {
				errs = append(errs, ValidationError{
					"secrets",
					"secret references do not take default or replacement operands; supply the fallback outside the stack",
				})
			}
		}
		if s.Extends != "" || declared[ref.Provider] {
			continue
		}
		msg := fmt.Sprintf("references undeclared secret provider %q; declare it under secrets.providers", ref.Provider)
		sites := s.References[ref.Key]
		if len(sites) == 0 {
			errs = append(errs, ValidationError{"secrets", msg})
			continue
		}
		for _, c := range sites {
			errs = append(errs, ValidationError{secretConsumerField(s, c), msg})
		}
	}
	return errs
}

func secretConsumerField(s *Stack, c Consumer) string {
	switch c.Kind {
	case RefKindMCPServer:
		for i := range s.MCPServers {
			if s.MCPServers[i].Name == c.Name {
				return fmt.Sprintf("mcp-servers[%d].%s", i, c.Field)
			}
		}
		return "mcp-servers." + c.Field
	case RefKindResource:
		for i := range s.Resources {
			if s.Resources[i].Name == c.Name {
				return fmt.Sprintf("resources[%d].%s", i, c.Field)
			}
		}
		return "resources." + c.Field
	case RefKindGateway:
		return "gateway." + c.Field
	case RefKindNetwork:
		if c.Name != "" {
			for i := range s.Networks {
				if s.Networks[i].Name == c.Name {
					return fmt.Sprintf("networks[%d].%s", i, c.Field)
				}
			}
		}
		return "network." + c.Field
	default:
		return c.Field
	}
}
