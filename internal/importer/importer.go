// Package importer converts MCP server entries found in client configs into
// gridctl stack entries. It is pure logic: callers (the import CLI) do all
// file I/O. The package normalizes the client dialect matrix (key spellings,
// transport names, command shapes), unwraps bridge entries, dedupes servers
// found in several clients, filters gridctl's own gateway entries, and
// classifies plaintext secrets so the CLI can offer vault moves.
package importer

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/provisioner"
)

// Skip reasons attached to candidates that will not be imported.
const (
	SkipGatewaySelfEntry = "gateway_self_entry"
	SkipUnsupported      = "unsupported_entry"
	SkipNameCollision    = "name_collision"
)

// Candidate is one importable server assembled from client config entries.
type Candidate struct {
	Name       string
	Server     config.MCPServer
	FoundIn    []string // client slugs the server was found in, sorted
	Source     string   // canonical slug (first client in registry order)
	Warnings   []string
	SkipReason string   // empty means importable
	SecretKeys []string // env keys whose literal values look like secrets
}

// MapEntry converts one raw client entry into a config.MCPServer plus
// warnings. An error means the entry cannot be represented (for example a
// websocket transport) and should surface as a skipped candidate.
func MapEntry(slug string, entry provisioner.ServerEntry) (config.MCPServer, []string, error) {
	raw := flattenTransportObject(entry.Raw)
	var warnings []string

	name, renamed := sanitizeName(entry.Name)
	if renamed {
		warnings = append(warnings, fmt.Sprintf("renamed %q to %q (whitespace is not usable in tool prefixes)", entry.Name, name))
	}

	server := config.MCPServer{Name: name}

	transport, transportErr := normalizeTransport(raw)
	if transportErr != nil {
		return server, warnings, transportErr
	}

	if remote := firstString(raw, "httpUrl", "url", "serverUrl", "uri"); remote != "" {
		server.URL = remote
		server.Transport = transport
		if transport == "" {
			server.Transport = transportFromURL(remote)
			if server.Transport == "sse" {
				warnings = append(warnings, "transport inferred as SSE from the URL path; verify after import")
			}
		}
		if _, ok := raw["httpUrl"]; ok {
			server.Transport = "http"
		}
		auth, authWarnings := mapHeaders(raw)
		server.Auth = auth
		warnings = append(warnings, authWarnings...)
		return server, warnings, nil
	}

	command := commandSlice(raw)
	if len(command) == 0 {
		return server, warnings, fmt.Errorf("entry has neither a command nor a URL")
	}
	command = unwrapCmdC(command)

	if bridgeURL, ok := unwrapMCPRemote(command); ok {
		server.URL = bridgeURL
		server.Transport = transportFromURL(bridgeURL)
		warnings = append(warnings, "converted an mcp-remote bridge into a direct URL server; verify the transport after import")
		return server, warnings, nil
	}

	server.Command = command
	server.Transport = "stdio"
	server.Env = envMap(raw)
	return server, warnings, nil
}

// IsGatewaySelfEntry reports whether a scanned entry is gridctl's own gateway
// connection. The entry key matching the link server name is the primary
// signal; a localhost URL pointing at the gateway's /sse or /mcp endpoints
// (directly or through an mcp-remote bridge) is the secondary one. A user's
// own localhost server under a different name and path is NOT flagged.
func IsGatewaySelfEntry(entryName, linkServerName string, raw map[string]any) bool {
	if entryName == linkServerName {
		return true
	}
	raw = flattenTransportObject(raw)
	if u := firstString(raw, "httpUrl", "url", "serverUrl", "uri"); u != "" {
		return isGatewayURL(u)
	}
	if bridgeURL, ok := unwrapMCPRemote(unwrapCmdC(commandSlice(raw))); ok {
		return isGatewayURL(bridgeURL)
	}
	return false
}

// Dedupe collapses candidates with the same identity (name plus URL or
// command line) into one, merging provenance. Candidates arrive in registry
// order, so the first occurrence is the canonical source. A same-name,
// different-definition candidate stays separate and is flagged for review.
func Dedupe(candidates []Candidate) []Candidate {
	var out []Candidate
	index := make(map[string]int)
	byName := make(map[string]string) // name -> identity of first definition
	for _, c := range candidates {
		id := identity(c)
		if i, ok := index[id]; ok {
			out[i].FoundIn = mergeSlug(out[i].FoundIn, c.FoundIn...)
			continue
		}
		if firstID, ok := byName[c.Name]; ok && firstID != id {
			c.Warnings = append(c.Warnings,
				fmt.Sprintf("a different definition of %q was also found in %s; review before importing both", c.Name, strings.Join(c.FoundIn, ", ")))
		} else {
			byName[c.Name] = id
		}
		index[id] = len(out)
		out = append(out, c)
	}
	return out
}

// ClassifySecretKeys returns the env keys whose values are literal secrets:
// non-empty, not a recognized reference, with a secret-suggestive key name.
// Sorted for deterministic prompts and output.
func ClassifySecretKeys(env map[string]string) []string {
	var keys []string
	for k, v := range env {
		if v == "" || IsReferenceValue(v) {
			continue
		}
		if secretKeyRe.MatchString(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// IsReferenceValue reports whether an env value is a reference that must be
// preserved verbatim rather than treated as a secret literal: interpolation
// dialects (${env:VAR}, ${input:id}, ${file:...}, ${VAR}, ${{ secrets.X }}),
// bare $VAR and %VAR%, 1Password op:// URIs, and gridctl's own ${var:KEY}.
func IsReferenceValue(v string) bool {
	v = strings.TrimSpace(v)
	switch {
	case strings.HasPrefix(v, "${"), strings.HasPrefix(v, "$"):
		return true
	case strings.HasPrefix(v, "op://"):
		return true
	case strings.HasPrefix(v, "%") && strings.HasSuffix(v, "%") && len(v) > 2:
		return true
	}
	return false
}

var secretKeyRe = regexp.MustCompile(`(?i)(token|secret|key|password|passphrase|credential|auth)`)

// --- internal helpers ---

// flattenTransportObject merges Continue-style nested transport objects
// ({name, transport: {type, url, command}}) into a flat entry map.
func flattenTransportObject(raw map[string]any) map[string]any {
	t, ok := raw["transport"].(map[string]any)
	if !ok {
		return raw
	}
	flat := make(map[string]any, len(raw)+len(t))
	for k, v := range raw {
		if k == "transport" {
			continue
		}
		flat[k] = v
	}
	for k, v := range t {
		if _, exists := flat[k]; !exists {
			flat[k] = v
		}
	}
	return flat
}

// normalizeTransport folds the client transport-type spellings onto
// gridctl's http/sse/stdio vocabulary. Empty string means unspecified.
func normalizeTransport(raw map[string]any) (string, error) {
	t := firstString(raw, "type", "transportType")
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "":
		return "", nil
	case "sse":
		return "sse", nil
	case "http", "streamable-http", "streamablehttp", "streamable_http", "streamable", "remote":
		return "http", nil
	case "stdio", "local":
		return "stdio", nil
	case "ws", "websocket":
		return "", fmt.Errorf("websocket transport is not supported")
	case "builtin", "platform", "frontend", "inline_python":
		return "", fmt.Errorf("client-internal extension type %q cannot be imported", t)
	default:
		return "", nil // unknown spellings fall back to shape inference
	}
}

// transportFromURL infers SSE from a /sse path suffix, else streamable HTTP.
func transportFromURL(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil && strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/sse") {
		return "sse"
	}
	return "http"
}

func isGatewayURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host != "localhost" && host != "127.0.0.1" {
		return false
	}
	path := strings.TrimRight(u.Path, "/")
	return strings.HasSuffix(path, "/sse") || strings.HasSuffix(path, "/mcp")
}

// commandSlice assembles the full command line from the entry's command (or
// Goose's cmd) plus args. A command string carrying embedded arguments
// (Cursor accepts "npx -y pkg" as one string) is shell-split.
func commandSlice(raw map[string]any) []string {
	cmd := firstString(raw, "command", "cmd")
	if cmd == "" {
		return nil
	}
	parts := shellSplit(cmd)
	if args, ok := raw["args"].([]any); ok {
		for _, a := range args {
			parts = append(parts, fmt.Sprintf("%v", a))
		}
	}
	return parts
}

// unwrapCmdC strips the Windows "cmd /c" (or "cmd.exe /c") wrapper so the
// wrapped command's identity survives import.
func unwrapCmdC(command []string) []string {
	if len(command) >= 3 {
		head := strings.ToLower(command[0])
		if (head == "cmd" || head == "cmd.exe") && strings.EqualFold(command[1], "/c") {
			return command[2:]
		}
	}
	return command
}

// unwrapMCPRemote recognizes the npx mcp-remote bridge shape and returns the
// bridged URL: [npx] [-y|--yes]... mcp-remote <url> [flags...].
func unwrapMCPRemote(command []string) (string, bool) {
	i := 0
	if i < len(command) && command[i] == "npx" {
		i++
	}
	for i < len(command) && (command[i] == "-y" || command[i] == "--yes") {
		i++
	}
	if i >= len(command) || command[i] != "mcp-remote" {
		return "", false
	}
	for _, arg := range command[i+1:] {
		if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
			return arg, true
		}
	}
	return "", false
}

// mapHeaders converts a client entry's headers object into gridctl's
// single-header auth model: an Authorization bearer header becomes bearer
// auth, exactly one other header becomes custom-header auth, and anything
// beyond that is reported rather than guessed at.
func mapHeaders(raw map[string]any) (*config.ServerAuth, []string) {
	headers, ok := raw["headers"].(map[string]any)
	if !ok || len(headers) == 0 {
		return nil, nil
	}
	for name, v := range headers {
		if strings.EqualFold(name, "authorization") {
			value := fmt.Sprintf("%v", v)
			token := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "Bearer"))
			if token != value && token != "" {
				warnings := []string{"Authorization header imported as bearer auth; consider a ${var:KEY} reference for the token"}
				if len(headers) > 1 {
					warnings = append(warnings, fmt.Sprintf("%d additional header(s) were not imported (gridctl supports one auth header)", len(headers)-1))
				}
				return &config.ServerAuth{Type: "bearer", Token: token}, warnings
			}
		}
	}
	if len(headers) == 1 {
		for name, v := range headers {
			return &config.ServerAuth{Type: "header", Header: name, Value: fmt.Sprintf("%v", v)},
				[]string{"header imported as custom-header auth; consider a ${var:KEY} reference for the value"}
		}
	}
	return nil, []string{fmt.Sprintf("%d header(s) were not imported (gridctl supports one auth header per server)", len(headers))}
}

func envMap(raw map[string]any) map[string]string {
	src, ok := raw["env"].(map[string]any)
	if !ok {
		src, ok = raw["envs"].(map[string]any)
	}
	if !ok || len(src) == 0 {
		return nil
	}
	env := make(map[string]string, len(src))
	for k, v := range src {
		env[k] = fmt.Sprintf("%v", v)
	}
	return env
}

func firstString(raw map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := raw[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func sanitizeName(name string) (string, bool) {
	sanitized := strings.Join(strings.Fields(name), "_")
	return sanitized, sanitized != name
}

func identity(c Candidate) string {
	if c.Server.URL != "" {
		return c.Name + "|url|" + c.Server.URL
	}
	return c.Name + "|cmd|" + strings.Join(c.Server.Command, "\x00")
}

func mergeSlug(existing []string, add ...string) []string {
	seen := make(map[string]bool, len(existing)+len(add))
	for _, s := range existing {
		seen[s] = true
	}
	for _, s := range add {
		if !seen[s] {
			seen[s] = true
			existing = append(existing, s)
		}
	}
	sort.Strings(existing)
	return existing
}

// shellSplit splits a command string on whitespace while respecting single
// and double quotes, enough for the command lines that appear in client
// configs. It does not implement escapes beyond quoting.
func shellSplit(s string) []string {
	var parts []string
	var b strings.Builder
	var quote rune
	flush := func() {
		if b.Len() > 0 {
			parts = append(parts, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t':
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return parts
}
