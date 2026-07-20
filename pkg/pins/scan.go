package pins

// Tool-poisoning heuristics run when a pin is first taken and when drift is
// presented for approval. Findings are advisory: they inform the human
// reviewing a pin, they never block anything, and static heuristics are one
// detection layer, not a complete defense (attacks that live in runtime tool
// OUTPUT are invisible to any pin-time gate by construction).
//
// Pattern banks P001-P003 are derived from Lasso Security's mcp-gateway
// (github.com/lasso-security/mcp-gateway, mcp_gateway/security_scanner/
// tool_poisoning_analyzer.py), MIT License, Copyright (c) 2024 Lasso
// Security, Inc. The P004 word list follows the taxonomy published in Snyk's
// agent-scan documentation (Apache License 2.0).

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// Finding codes. Stable identifiers usable in scan_ignore.
const (
	CodeHiddenInstructions = "P001" // hidden-instruction phrases
	CodeSensitiveFiles     = "P002" // sensitive file references
	CodeSensitiveActions   = "P003" // sensitive-action language
	CodeSuspiciousWords    = "P004" // suspicious emphasis words
	CodeHiddenUnicode      = "P005" // invisible characters, decoded payloads
	CodeToolShadowing      = "P006" // references to other servers' tools
)

// Severity levels for findings, matching the info/warn/critical vocabulary
// used across gridctl (pkg/optimize, the web UI severity cards).
const (
	SeverityInfo     = "info"
	SeverityWarn     = "warn"
	SeverityCritical = "critical"
)

// Confidence levels for findings.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// Finding is one poisoning signal detected in a tool definition. Snippet is
// raw matched text: every rendering surface must escape it (the CLI and web
// UI both route it through their escapeNonPrintable helpers).
type Finding struct {
	Code       string `json:"code"`
	Severity   string `json:"severity"`
	Confidence string `json:"confidence"`
	Field      string `json:"field"`
	Snippet    string `json:"snippet,omitempty"`
	Message    string `json:"message"`
	Decoded    string `json:"decoded,omitempty"`
}

const (
	fieldName         = "name"
	fieldDescription  = "description"
	fieldInputSchema  = "input_schema"
	fieldOutputSchema = "output_schema"

	snippetMax      = 120
	maxFindingsTool = 16
	// suspiciousWordsMin is the number of distinct P004 words required before
	// a finding is emitted; a single "important" in a description is noise.
	suspiciousWordsMin = 2
	// invisibleCriticalKinds escalates P005 to critical when this many
	// distinct hidden-character categories co-occur.
	invisibleCriticalKinds = 3
)

// hiddenInstructionPatterns is Lasso bank A: phrasing that instructs the model
// to conceal behavior from the user.
var hiddenInstructionPatterns = compileBank([]string{
	`(ignore|disregard) (earlier|previous|prior) (instructions|constraints)`,
	`bypass (security|restrictions|limitations)`,
	`\b(don't|do not) (tell|inform|alert|notify|mention)\s+(anyone|the user|them|others|somebody)`,
	`(keep|maintain) (this|these) (secret|hidden)`,
	`(this is between us|our secret)`,
	`(deleted|ignore|skip) this (after|once) reading`,
	`(silently|secretly) (execute|perform|run|do|implement)`,
	`instructions? (are|is) confidential`,
	`this (will be|is) our (little|private) secret`,
	`following instructions? (should|must) be (hidden|concealed|obscured)`,
	`(pretend|act) (like|as if) (you (are|were) just|you're just)`,
	`don't (make (it|this)|let (it|this) be) (obvious|apparent|evident|clear|noticeable)`,
	`(without|don't) (alerting|informing|notifying) (the|any|other) (user|person|people|individuals)`,
})

// sensitiveFilePatterns is Lasso bank B: paths and key names a tool
// description has no business referencing.
var sensitiveFilePatterns = compileBank([]string{
	`\.env\b`,
	`(?:config(?:_|\.)secrets?)\.(?:json|yaml|yml|xml|txt|properties)`,
	`\.pem\b`,
	`(?:password|credentials)\.(?:txt|json|yaml|yml|xml)`,
	`(?:ssh|private)(?:_|\.)key`,
	`\.htpasswd`,
	`id_rsa`,
	`\.aws/`,
	`\.ssh/`,
	`token\.(?:json|yaml|yml|xml|txt|properties)`,
	`api[_\-\.]?key`,
	`auth(?:entication)?[_\-\.]?(?:token|key)`,
	`oauth[_\-\.]?(?:token|key)`,
	`/etc/(?:passwd|hosts|sudoers|shadow)`,
	`keystore\.(?:jks|p12|pkcs12)`,
})

// sensitiveActionPatterns is Lasso bank C: destructive or exfiltration
// language. Info-tier by design: legitimate tools describe these capabilities.
var sensitiveActionPatterns = compileBank([]string{
	`(execute|run|spawn|invoke) (shell|bash|powershell|cmd) (command|script)s?`,
	`(delete|remove|drop) (database|table|collection|index)`,
	`(exec\(|eval\(|subprocess\.|\bos\.system\(|\bshell=True\b|javascript:|\bRuntime\.exec\(|\bnew\s+Function\()`,
	`(install|inject)[_\-\s]?(rootkit|malware|backdoor)`,
	`(disable|bypass)[_\-\s]?(firewall|antivirus|security)`,
	`privilege[_\-\s]?escalation`,
	`data[_\-\s]?(exfiltration|theft)`,
	`(reverse|bind)[_\-\s]?shell`,
	`remote[_\-\s]?code[_\-\s]?execution`,
	`brute[_\-\s]?force`,
})

// suspiciousWords is the emphasis vocabulary that steers model attention.
var suspiciousWords = regexp.MustCompile(`(?i)\b(important|crucial|critical|vital|urgent|ignore|disregard|override|bypass)\b`)

// quotedSpans locates single- or double-quoted runs for position discounting.
var quotedSpans = regexp.MustCompile(`"[^"\n]{1,200}"|'[^'\n]{1,200}'`)

func compileBank(patterns []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		out = append(out, regexp.MustCompile(`(?i)`+p))
	}
	return out
}

// ScanTool runs all pin-time checks (P001-P005) over one tool definition:
// name, description, and the decoded string values of both schemas (injection
// also hides in parameter names, enums, and defaults, so schema keys are
// scanned too). P006 needs the cross-server inventory and lives in
// ScanShadowing.
func ScanTool(t mcp.Tool) []Finding {
	var findings []Finding
	findings = append(findings, scanText(fieldName, t.Name)...)
	findings = append(findings, scanText(fieldDescription, t.Description)...)
	if blob := schemaText(t.InputSchema); blob != "" {
		findings = append(findings, scanText(fieldInputSchema, blob)...)
	}
	if blob := schemaText(t.OutputSchema); blob != "" {
		findings = append(findings, scanText(fieldOutputSchema, blob)...)
	}

	sortFindings(findings)
	if len(findings) > maxFindingsTool {
		findings = findings[:maxFindingsTool]
	}
	return findings
}

// ScanShadowing implements P006: a tool description that names another
// server's tools (or the server itself) can steer the model's use of that
// trusted server. inventory maps server name to tool names; self is excluded.
// Only names of five or more characters are matched, on word boundaries, to
// keep generic tool names ("search", "run") from flagging everything.
// Matching is plain case-insensitive substring search with boundary checks
// (no per-name regexp compilation: this runs per tool on the pins read path),
// against the base normalization so literal digit-bearing names ("route53")
// are not corrupted by the leetspeak fold.
func ScanShadowing(t mcp.Tool, self string, inventory map[string][]string) []Finding {
	text := strings.ToLower(normalizeBase(t.Description))
	if text == "" {
		return nil
	}
	var findings []Finding
	servers := make([]string, 0, len(inventory))
	for name := range inventory {
		servers = append(servers, name)
	}
	sort.Strings(servers)
	for _, server := range servers {
		if server == self {
			continue
		}
		for _, name := range append([]string{server}, inventory[server]...) {
			if len(name) < 5 {
				continue
			}
			loc := findWord(text, strings.ToLower(name))
			if loc == nil {
				continue
			}
			findings = append(findings, Finding{
				Code:       CodeToolShadowing,
				Severity:   SeverityWarn,
				Confidence: ConfidenceMedium,
				Field:      fieldDescription,
				Snippet:    snippetAround(text, loc),
				Message:    fmt.Sprintf("description references %q from server %q; cross-server references can steer a trusted server's tools", name, server),
			})
			break // one finding per referenced server
		}
	}
	return findings
}

// findWord locates word (already lowercased) in text (already lowercased) at
// a position where neither neighbor is a word character, returning the byte
// span or nil. Equivalent to the \bword\b regexp without compiling one.
func findWord(text, word string) []int {
	if word == "" {
		return nil
	}
	for start := 0; ; {
		idx := strings.Index(text[start:], word)
		if idx < 0 {
			return nil
		}
		lo := start + idx
		hi := lo + len(word)
		if !wordCharBefore(text, lo) && !wordCharAt(text, hi) {
			return []int{lo, hi}
		}
		start = lo + 1
	}
}

func wordCharBefore(s string, i int) bool {
	if i == 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return isWordRune(r)
}

func wordCharAt(s string, i int) bool {
	if i >= len(s) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return isWordRune(r)
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// FilterFindings drops findings whose code appears in ignore.
func FilterFindings(findings []Finding, ignore []string) []Finding {
	if len(ignore) == 0 || len(findings) == 0 {
		return findings
	}
	drop := make(map[string]bool, len(ignore))
	for _, code := range ignore {
		drop[strings.ToUpper(strings.TrimSpace(code))] = true
	}
	out := findings[:0:0]
	for _, f := range findings {
		if !drop[f.Code] {
			out = append(out, f)
		}
	}
	return out
}

// MaxSeverity returns the highest severity present, or "" for no findings.
func MaxSeverity(findings []Finding) string {
	max := ""
	for _, f := range findings {
		if SeverityRank(f.Severity) > SeverityRank(max) {
			max = f.Severity
		}
	}
	return max
}

// SeverityRank orders finding severities for threshold comparison: critical
// outranks warn outranks info; unknown values rank lowest. Exported because
// severity ordering is a property of the finding vocabulary, and callers
// (the CLI's --fail-on-findings gate) should consume it rather than re-encode it.
func SeverityRank(s string) int {
	switch s {
	case SeverityCritical:
		return 3
	case SeverityWarn:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}

// scanText runs P005 on the raw text, then P001-P004 on the normalized text.
// Banks match against both the base normalization and its leetspeak fold:
// the fold defeats "1gn0re"-style evasion but corrupts digit-bearing pattern
// literals ("p12" folds to "pi2"), so neither form alone is sufficient. Both
// forms are byte-length identical, so quoted spans computed on the base apply
// to fold matches too.
func scanText(field, raw string) []Finding {
	if raw == "" {
		return nil
	}
	var findings []Finding

	if rep := detectInvisible(raw); rep.count > 0 {
		f := Finding{
			Code:       CodeHiddenUnicode,
			Severity:   SeverityWarn,
			Confidence: ConfidenceHigh,
			Field:      field,
			Message:    fmt.Sprintf("%d hidden character(s) present (%s)", rep.count, strings.Join(rep.kinds, ", ")),
			Decoded:    rep.decoded,
		}
		if rep.decoded != "" || len(rep.kinds) >= invisibleCriticalKinds {
			f.Severity = SeverityCritical
			if rep.decoded != "" {
				f.Message = fmt.Sprintf("hidden Unicode tag characters decode to a smuggled message (%d hidden character(s), %s)", rep.count, strings.Join(rep.kinds, ", "))
			}
		}
		findings = append(findings, f)
	}

	base := normalizeBase(raw)
	folded := leetFold(base)
	quoted := quotedSpans.FindAllStringIndex(base, -1)

	findings = append(findings, matchBank(field, base, folded, quoted, hiddenInstructionPatterns,
		CodeHiddenInstructions, SeverityWarn, ConfidenceHigh,
		"hidden-instruction phrasing")...)
	findings = append(findings, matchBank(field, base, folded, quoted, sensitiveFilePatterns,
		CodeSensitiveFiles, SeverityWarn, ConfidenceMedium,
		"reference to a sensitive file or credential")...)
	findings = append(findings, matchBank(field, base, folded, quoted, sensitiveActionPatterns,
		CodeSensitiveActions, SeverityInfo, ConfidenceLow,
		"sensitive-action language")...)

	if words := distinctMatches(suspiciousWords, base); len(words) >= suspiciousWordsMin {
		findings = append(findings, Finding{
			Code:       CodeSuspiciousWords,
			Severity:   SeverityInfo,
			Confidence: ConfidenceLow,
			Field:      field,
			Snippet:    strings.Join(words, ", "),
			Message:    "emphasis words that steer model attention",
		})
	}

	return findings
}

// matchBank emits at most one finding per pattern, trying the base
// normalization first and the leetspeak fold second. A match inside a quoted
// span is discounted to info/low: text that QUOTES an attack phrase (a
// security tool documenting what it detects) is describing, not injecting.
func matchBank(field, base, folded string, quoted [][]int, bank []*regexp.Regexp, code, severity, confidence, label string) []Finding {
	var findings []Finding
	for _, re := range bank {
		text := base
		loc := re.FindStringIndex(base)
		if loc == nil {
			text = folded
			loc = re.FindStringIndex(folded)
		}
		if loc == nil {
			continue
		}
		f := Finding{
			Code:       code,
			Severity:   severity,
			Confidence: confidence,
			Field:      field,
			Snippet:    snippetAround(text, loc),
			Message:    label,
		}
		if insideAny(loc, quoted) {
			f.Severity = SeverityInfo
			f.Confidence = ConfidenceLow
			f.Message = label + " (quoted; likely descriptive)"
		}
		findings = append(findings, f)
	}
	return findings
}

// schemaText decodes a raw JSON schema and joins every string it contains,
// keys included, with newlines. Scanning decoded values (rather than the
// marshaled document) matters for P005: JSON escapes like ​ hide raw
// invisible characters from a byte-level scan.
func schemaText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	var parts []string
	collectStrings(v, &parts)
	return strings.Join(parts, "\n")
}

func collectStrings(v any, out *[]string) {
	switch val := v.(type) {
	case string:
		*out = append(*out, val)
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			*out = append(*out, k)
			collectStrings(val[k], out)
		}
	case []any:
		for _, item := range val {
			collectStrings(item, out)
		}
	}
}

func distinctMatches(re *regexp.Regexp, s string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, m := range re.FindAllString(s, -1) {
		w := strings.ToLower(m)
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	sort.Strings(out)
	return out
}

func insideAny(loc []int, spans [][]int) bool {
	for _, span := range spans {
		if loc[0] >= span[0] && loc[1] <= span[1] {
			return true
		}
	}
	return false
}

// snippetAround returns the matched text capped to snippetMax runes.
func snippetAround(s string, loc []int) string {
	snippet := s[loc[0]:loc[1]]
	runes := []rune(snippet)
	if len(runes) > snippetMax {
		return string(runes[:snippetMax]) + "…"
	}
	return snippet
}

func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if SeverityRank(findings[i].Severity) != SeverityRank(findings[j].Severity) {
			return SeverityRank(findings[i].Severity) > SeverityRank(findings[j].Severity)
		}
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		return findings[i].Field < findings[j].Field
	})
}
