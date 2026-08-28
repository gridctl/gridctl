package config

// ReferenceKind identifies what kind of stack element references a variable.
type ReferenceKind string

const (
	RefKindMCPServer ReferenceKind = "mcp-server"
	RefKindResource  ReferenceKind = "resource"
	RefKindGateway   ReferenceKind = "gateway"
	RefKindNetwork   ReferenceKind = "network"
	RefKindStack     ReferenceKind = "stack"
	// RefKindSecretsSet marks a synthetic consumer for a variable injected in
	// bulk through the stack's secrets.sets block (see injectSetSecrets). These
	// never appear in Stack.References — the API layer synthesizes them from
	// vault set membership so usage reporting covers injected keys too.
	RefKindSecretsSet ReferenceKind = "secrets-set"
)

// Consumer is a single site that references a variable: the kind of stack
// element, its name (server/resource/network name; empty for stack- and
// gateway-level sites), and the field where the reference appears.
//
// Field mirrors the YAML key path the user actually wrote
// (e.g. "env.GITHUB_TOKEN", "image", "command[2]", "ssh.identityFile",
// "openapi.baseUrl") so it can be used verbatim to locate the reference in the
// stack file. Casing therefore tracks the schema's own YAML tags, which mix
// camelCase (identityFile, baseUrl) and snake_case (build_args, ssh_key_path).
type Consumer struct {
	Kind   ReferenceKind `json:"kind"`
	Name   string        `json:"name,omitempty"`
	Field  string        `json:"field"`
	Source string        `json:"source,omitempty"`

	// Target names the workload a scoped secrets.sets entry injects into, and
	// TargetKind says whether that is a server or a resource. Both are set only
	// on RefKindSecretsSet consumers built from a scoped set: one such consumer
	// is synthesized per receiving workload, so the UI can name and navigate to
	// each one. An unscoped set fans out to everything and produces a single
	// consumer with both fields empty.
	//
	// Name keeps holding the set name in every case. Callers that ask "is this
	// variable's own set actively injected" compare against Name, so widening
	// that field to mean the workload would silently break them.
	Target     string        `json:"target,omitempty"`
	TargetKind ReferenceKind `json:"targetKind,omitempty"`
}

// BuildVariableUsage combines explicit references with synthetic set
// consumers. setMembers maps set names to value-free variable keys.
func BuildVariableUsage(spec *Stack, setMembers map[string][]string) (ReferenceIndex, bool) {
	usage := ReferenceIndex{}
	if spec == nil {
		return usage, true
	}
	for key, consumers := range spec.References {
		usage[key] = append([]Consumer(nil), consumers...)
	}
	complete := true
	if spec.Secrets == nil || len(spec.Secrets.Sets) == 0 {
		return usage, complete
	}
	if setMembers == nil {
		return usage, false
	}
	for _, ref := range spec.Secrets.Sets {
		consumers := SetConsumersFor(ref, spec)
		for _, key := range setMembers[ref.Name] {
			for _, c := range consumers {
				found := false
				for _, existing := range usage[key] {
					if existing == c {
						found = true
						break
					}
				}
				if !found {
					usage[key] = append(usage[key], c)
				}
			}
		}
	}
	return usage, complete
}

// SetConsumersFor returns the synthetic consumers contributed by one set ref.
func SetConsumersFor(ref SecretSetRef, spec *Stack) []Consumer {
	base := Consumer{Kind: RefKindSecretsSet, Name: ref.Name, Field: "secrets.sets"}
	if !ref.Scoped() {
		return []Consumer{base}
	}
	var out []Consumer
	for _, srv := range spec.MCPServers {
		if ref.InjectsIntoServer(srv.Name) {
			c := base
			c.Target = srv.Name
			c.TargetKind = RefKindMCPServer
			out = append(out, c)
		}
	}
	for _, res := range spec.Resources {
		if ref.InjectsIntoResource(res.Name) {
			c := base
			c.Target = res.Name
			c.TargetKind = RefKindResource
			out = append(out, c)
		}
	}
	return out
}

// ReferenceIndex maps a variable-store key to the consumers that reference it.
//
// It is built by expandStackVars from the same grammar used for expansion
// (ExpandStringRefs), so it can never drift from what gridctl actually
// recognizes as a ${var:KEY}/${vault:KEY} reference. It carries only keys and
// reference-site metadata — never variable values — so it is safe to expose
// even while the vault is locked.
//
// Scope:
//   - One-hop only: a variable referenced inside another variable's *value* is
//     not followed. This is intentional — values live in the vault, outside the
//     static stack, so resolving transitive references would require reading
//     secrets at index time. v1 indexes only references written in the stack.
//   - Known gap: secrets injected via secrets.sets (see injectSetSecrets) are
//     added to server env *after* expansion without ${var:KEY} syntax, so they
//     are not recorded here. The usage API compensates by synthesizing
//     RefKindSecretsSet consumers from vault set membership; the index itself
//     stays explicit-references-only.
type ReferenceIndex map[string][]Consumer

// add records that the consumer c references key, de-duplicating exact
// (kind, name, field) repeats so a value like "${var:X}-${var:X}" is counted
// once per field.
func (idx ReferenceIndex) add(key string, c Consumer) {
	for _, existing := range idx[key] {
		if existing == c {
			return
		}
	}
	idx[key] = append(idx[key], c)
}
