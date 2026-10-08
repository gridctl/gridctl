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
	// SkipDisabled means an OpenCode entry set enabled to false. Other clients
	// ignore the field.
	SkipDisabled = "disabled"
	// SkipUntransferredOption means dropping the option would change execution.
	// OpenCode skips a working directory on every shape, and skips oauth or
	// local headers on a command array.
	SkipUntransferredOption = "untransferred_option"
	// SkipNativeExpression means a newly supported native value used an
	// OpenCode expression Gridctl cannot represent without reading it.
	SkipNativeExpression = "unsupported_native_expression"
	// SkipAmbiguous means two native forms conflict and were not merged.
	SkipAmbiguous = "ambiguous_entry"
)

// openCodeSlug is the provisioner slug whose native local shape this package
// maps. Other clients keep the string-command dialect.
const openCodeSlug = "opencode"

// MapError is a value-free reason an entry cannot be imported. Detail must
// not include config values, expression text, or referenced file contents.
type MapError struct {
	Reason string
	Detail string
}

func (e *MapError) Error() string {
	if e == nil {
		return ""
	}
	if e.Detail != "" {
		return e.Detail
	}
	return e.Reason
}

// Origin is one place a candidate was read from. Scope is project, local,
// custom, or user.
type Origin struct {
	Client string
	Scope  string
	Path   string
}

// MapOptions carries project-import context. Empty options keep MapEntry
// behavior unchanged.
type MapOptions struct {
	OwnerDir string
	Scope    string
}

// Candidate is one importable server assembled from client config entries.
type Candidate struct {
	Name        string
	Server      config.MCPServer
	FoundIn     []string // client slugs the server was found in, sorted
	Source      string   // canonical slug (first client in registry order)
	SourcePath  string   // canonical config file path; not a client slug
	SourcePaths []string // config file paths retained through dedupe
	Origins     []Origin // client, scope, and path triples in append order
	Warnings    []string
	SkipReason  string   // empty means importable
	SecretKeys  []string // env keys whose literal values look like secrets
}

// Scopes returns the sorted unique scopes recorded on Origins.
func (c Candidate) Scopes() []string {
	return scopesFrom(c.Origins, "")
}

// ScopesFor returns the sorted unique scopes recorded for client.
func (c Candidate) ScopesFor(client string) []string {
	return scopesFrom(c.Origins, client)
}

// ProvenanceLabel groups origins by client in FoundIn order and renders
// slug (scope) pairs. Several scopes for one client join with +.
func (c Candidate) ProvenanceLabel() string {
	if len(c.Origins) == 0 {
		return strings.Join(c.FoundIn, ", ")
	}
	order := append([]string(nil), c.FoundIn...)
	seenClient := make(map[string]bool, len(order))
	for _, slug := range order {
		seenClient[slug] = true
	}
	for _, o := range c.Origins {
		if o.Client != "" && !seenClient[o.Client] {
			seenClient[o.Client] = true
			order = append(order, o.Client)
		}
	}
	var parts []string
	for _, client := range order {
		var scopes []string
		seenScope := map[string]bool{}
		for _, o := range c.Origins {
			if o.Client != client || o.Scope == "" || seenScope[o.Scope] {
				continue
			}
			seenScope[o.Scope] = true
			scopes = append(scopes, o.Scope)
		}
		if len(scopes) == 0 {
			parts = append(parts, client)
			continue
		}
		parts = append(parts, client+" ("+strings.Join(scopes, "+")+")")
	}
	return strings.Join(parts, ", ")
}

func scopesFrom(origins []Origin, client string) []string {
	seen := map[string]bool{}
	var out []string
	for _, o := range origins {
		if client != "" && o.Client != client {
			continue
		}
		if o.Scope == "" || seen[o.Scope] {
			continue
		}
		seen[o.Scope] = true
		out = append(out, o.Scope)
	}
	sort.Strings(out)
	return out
}

// MapEntry converts one raw client entry into a config.MCPServer plus
// warnings. An error means the entry cannot be represented (for example a
// websocket transport) and should surface as a skipped candidate.
func MapEntry(slug string, entry provisioner.ServerEntry) (config.MCPServer, []string, error) {
	return MapEntryWithOptions(slug, entry, MapOptions{})
}

// MapEntryWithOptions is MapEntry with project placeholder substitution and
// scope-dependent working-directory handling. Empty options match MapEntry.
func MapEntryWithOptions(slug string, entry provisioner.ServerEntry, opts MapOptions) (config.MCPServer, []string, error) {
	raw := entry.Raw
	var pre []string
	var st *mapState
	if opts.Scope != "" && slug != openCodeSlug {
		warning, err := nonOpenCodeCwd(flattenTransportObject(raw), opts.Scope)
		if err != nil {
			server := config.MCPServer{}
			server.Name, _ = sanitizeName(entry.Name)
			return server, nil, err
		}
		if warning != "" {
			pre = append(pre, warning)
		}
	}
	if opts.OwnerDir != "" {
		raw = cloneEntryMap(flattenTransportObject(raw))
		st = &mapState{ownerDir: opts.OwnerDir}
		if err := substituteMappedFields(raw, st); err != nil {
			server := config.MCPServer{}
			server.Name, _ = sanitizeName(entry.Name)
			return server, append(pre, st.warnings()...), err
		}
	}
	server, warnings, err := mapEntryPrepared(slug, entry, raw, st)
	if st != nil {
		warnings = append(warnings, st.warnings()...)
	}
	if len(pre) > 0 {
		warnings = append(warnings, pre...)
	}
	return server, warnings, err
}

func mapEntryPrepared(slug string, entry provisioner.ServerEntry, raw map[string]any, st *mapState) (config.MCPServer, []string, error) {
	raw = flattenTransportObject(raw)
	var warnings []string

	name, renamed := sanitizeName(entry.Name)
	if renamed {
		warnings = append(warnings, fmt.Sprintf("renamed %q to %q (whitespace is not usable in tool prefixes)", entry.Name, name))
	}

	server := config.MCPServer{Name: name}

	// OpenCode enabled and cwd apply before the URL return. A remote entry
	// with either set must skip, not import as an ordinary HTTP server.
	if slug == openCodeSlug {
		if err := openCodeExecutionConstraints(raw); err != nil {
			return server, warnings, err
		}
	}

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

	if slug == openCodeSlug && openCodeNative(raw) {
		return mapOpenCodeNative(server, warnings, raw, st)
	}

	command := commandSlice(raw)
	if st != nil {
		var err error
		command, err = st.substituteArgv(command)
		if err != nil {
			return server, warnings, err
		}
	}
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
	byName := make(map[string]seenDef)
	for _, c := range candidates {
		id := identity(c)
		if i, ok := index[id]; ok {
			out[i].FoundIn = mergeSlug(out[i].FoundIn, c.FoundIn...)
			out[i].SourcePaths = mergePaths(out[i].SourcePaths, c.SourcePaths...)
			out[i].Origins = mergeOrigins(out[i].Origins, c.Origins...)
			if out[i].SourcePath == "" {
				out[i].SourcePath = c.SourcePath
			}
			continue
		}
		if first, ok := byName[c.Name]; ok && first.id != id {
			if originsDiffer(first.origins, c.Origins) {
				c.Warnings = append(c.Warnings, fmt.Sprintf(
					"a different definition of %q was also found in %s; the %s definition was kept",
					c.Name, c.ProvenanceLabel(), first.prov))
			} else {
				c.Warnings = append(c.Warnings, fmt.Sprintf(
					"a different definition of %q was also found in %s; review before importing both",
					c.Name, strings.Join(c.FoundIn, ", ")))
			}
		} else if !ok {
			byName[c.Name] = seenDef{id: id, prov: c.ProvenanceLabel(), origins: append([]Origin(nil), c.Origins...)}
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

type seenDef struct {
	id      string
	prov    string
	origins []Origin
}

func originsDiffer(a, b []Origin) bool {
	if len(a) == 0 && len(b) == 0 {
		return false
	}
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i] != b[i] {
			return true
		}
	}
	return false
}

func mergeOrigins(existing []Origin, add ...Origin) []Origin {
	seen := make(map[Origin]bool, len(existing)+len(add))
	for _, o := range existing {
		seen[o] = true
	}
	for _, o := range add {
		if seen[o] {
			continue
		}
		seen[o] = true
		existing = append(existing, o)
	}
	return existing
}

func mergePaths(existing []string, add ...string) []string {
	seen := make(map[string]bool, len(existing)+len(add))
	for _, s := range existing {
		seen[s] = true
	}
	for _, s := range add {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		existing = append(existing, s)
	}
	return existing
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
