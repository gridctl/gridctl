package config

// ReferenceKind identifies what kind of stack element references a variable.
type ReferenceKind string

const (
	RefKindMCPServer ReferenceKind = "mcp-server"
	RefKindResource  ReferenceKind = "resource"
	RefKindGateway   ReferenceKind = "gateway"
	RefKindNetwork   ReferenceKind = "network"
	RefKindStack     ReferenceKind = "stack"
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
	Kind  ReferenceKind `json:"kind"`
	Name  string        `json:"name,omitempty"`
	Field string        `json:"field"`
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
//     are not recorded here. v1 indexes explicit references only.
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
