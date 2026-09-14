package stackpolicy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

var knownRules = map[string]struct{}{
	RuleExplicitImageDigests:    {},
	RuleDenyLocalCommandServers: {},
	RuleDenySSHServers:          {},
	RuleSchemaPinningEnabled:    {},
	RuleSchemaPinningBlock:      {},
	RuleNonemptyServerToolLists: {},
}

type parsedPolicy struct {
	Version string
	Enabled []string
	Digest  string
	Bytes   []byte
}

type policyWire struct {
	Version string   `yaml:"version"`
	Enabled []string `yaml:"enabled"`
}

func readPolicy(ctx context.Context, path string) (*parsedPolicy, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, codeErr("policy-missing")
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, codeErr("policy-missing")
		}
		return nil, codeErr("policy-unreadable")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, codeErr("policy-symlink")
	}
	if !info.Mode().IsRegular() {
		return nil, codeErr("policy-unreadable")
	}
	if info.Size() > maxFileBytes {
		return nil, codeErr("policy-limit")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, codeErr("policy-unreadable")
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return nil, codeErr("policy-unreadable")
	}
	if int64(len(data)) > maxFileBytes {
		return nil, codeErr("policy-limit")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return parsePolicyBytes(data)
}

func parsePolicyBytes(data []byte) (*parsedPolicy, error) {
	sum := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	doc, err := decodeSingleDocument(data)
	if err != nil {
		return &parsedPolicy{Digest: digest, Bytes: data}, err
	}
	if err := rejectInterpolation(doc); err != nil {
		return &parsedPolicy{Digest: digest, Bytes: data}, codeErr("policy-interpolation")
	}
	root, err := documentMapping(doc)
	if err != nil {
		return &parsedPolicy{Digest: digest, Bytes: data}, codeErr("policy-invalid")
	}
	for _, key := range mappingKeys(root) {
		switch key {
		case "version", "enabled":
		default:
			return &parsedPolicy{Digest: digest, Bytes: data}, codeErr("policy-unknown-field")
		}
	}
	versionNode := mappingValue(root, "version")
	if versionNode == nil || isNull(versionNode) {
		return &parsedPolicy{Digest: digest, Bytes: data}, codeErr("policy-version")
	}
	if versionNode.Kind != yaml.ScalarNode || versionNode.Tag != "!!str" || versionNode.Value != PolicyVersion {
		return &parsedPolicy{Digest: digest, Bytes: data}, codeErr("policy-version")
	}
	enabledNode := mappingValue(root, "enabled")
	if enabledNode == nil || isNull(enabledNode) {
		return &parsedPolicy{Digest: digest, Bytes: data}, codeErr("policy-enabled")
	}
	if enabledNode.Kind != yaml.SequenceNode {
		return &parsedPolicy{Digest: digest, Bytes: data}, codeErr("policy-enabled")
	}
	if len(enabledNode.Content) == 0 {
		return &parsedPolicy{Digest: digest, Bytes: data}, codeErr("policy-enabled")
	}
	enabled := make([]string, 0, len(enabledNode.Content))
	seen := make(map[string]struct{}, len(enabledNode.Content))
	for _, item := range enabledNode.Content {
		n := resolveAlias(item)
		if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
			return &parsedPolicy{Digest: digest, Bytes: data}, codeErr("policy-enabled")
		}
		id := n.Value
		if id == "" {
			return &parsedPolicy{Digest: digest, Bytes: data}, codeErr("policy-enabled")
		}
		if _, ok := knownRules[id]; !ok {
			return &parsedPolicy{Digest: digest, Bytes: data}, codeErr("policy-unknown-rule")
		}
		if _, ok := seen[id]; ok {
			return &parsedPolicy{Digest: digest, Bytes: data}, codeErr("policy-duplicate-rule")
		}
		seen[id] = struct{}{}
		enabled = append(enabled, id)
	}
	var wire policyWire
	if err := decodeKnownFields(data, &wire); err != nil {
		return &parsedPolicy{Digest: digest, Bytes: data}, codeErr("policy-unknown-field")
	}
	return &parsedPolicy{
		Version: PolicyVersion,
		Enabled: enabled,
		Digest:  digest,
		Bytes:   append([]byte(nil), data...),
	}, nil
}

func diagnosticMessage(code string) string {
	switch code {
	case "policy-missing":
		return "Policy file is missing or empty."
	case "policy-unreadable":
		return "Policy file could not be read."
	case "policy-symlink":
		return "Policy file must be a regular file."
	case "policy-limit":
		return "Policy file exceeds the size limit."
	case "policy-invalid":
		return "Policy document is not a supported version 1 selection."
	case "policy-version":
		return "Policy version must be the string \"1\"."
	case "policy-enabled":
		return "Policy enabled list must be a nonempty sequence of known rule IDs."
	case "policy-unknown-field":
		return "Policy document contains an unsupported field."
	case "policy-unknown-rule":
		return "Policy selects an unknown rule."
	case "policy-duplicate-rule":
		return "Policy selects a rule more than once."
	case "policy-interpolation":
		return "Policy document must not contain interpolations or expressions."
	case "empty-document":
		return "YAML document is empty."
	case "multiple-documents":
		return "Multiple YAML documents are not allowed."
	case "duplicate-key":
		return "Duplicate YAML keys are not allowed."
	case "yaml-invalid":
		return "YAML structure is invalid."
	case "yaml-depth":
		return "YAML nesting exceeds the parser depth limit."
	case "yaml-nodes":
		return "YAML node count exceeds the parser limit."
	case "interpolation":
		return "Interpolations are not allowed in this document."
	case "input-unreadable":
		return "Candidate stack could not be read."
	case "input-invalid":
		return "Candidate stack could not be interpreted."
	case "extends-escape":
		return "Extends path leaves the candidate root."
	case "extends-symlink":
		return "Extends path must not be a symbolic link."
	case "extends-cycle":
		return "Extends graph contains a cycle."
	case "extends-missing":
		return "Extends parent is missing."
	case "extends-depth":
		return "Extends chain exceeds the maximum inheritance depth."
	case "extends-absolute":
		return "Extends path must be a relative path inside the candidate root."
	case "extends-remote":
		return "Remote extends paths are not allowed."
	case "extends-dynamic":
		return "Extends path must be a literal relative path."
	case "extends-policy":
		return "Candidate data must not include the selected policy file."
	case "extends-nonregular":
		return "Extends path must be a regular file."
	case "extends-limit":
		return "Candidate input exceeds size or graph limits."
	case "malformed-server-kind":
		return "MCP server kind is malformed or mutually exclusive."
	case "malformed-resource":
		return "Supporting resource declaration is invalid."
	case "no-applicable-checks":
		return "Selected requirements produced no applicable checks."
	case "canceled":
		return "Evaluation was canceled."
	default:
		return "Policy evaluation could not be completed."
	}
}

func attachPolicyError(report *Report, err error) {
	code := errorCode(err)
	if err == context.Canceled || err == context.DeadlineExceeded {
		code = "canceled"
	}
	report.addDiagnostic(code, "policy", "", diagnosticMessage(code))
}

func attachInputError(report *Report, err error, source string) {
	code := errorCode(err)
	if err == context.Canceled || err == context.DeadlineExceeded {
		code = "canceled"
	}
	if source == "" {
		source = "entry"
	}
	report.addDiagnostic(code, source, "", diagnosticMessage(code))
}
