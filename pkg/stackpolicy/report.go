package stackpolicy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

const (
	PolicyVersion  = "1"
	CheckerVersion = "1"
	SchemaVersion  = 1
)

const (
	RuleExplicitImageDigests    = "explicit-image-digests"
	RuleDenyLocalCommandServers = "deny-local-command-servers"
	RuleDenySSHServers          = "deny-ssh-servers"
	RuleSchemaPinningEnabled    = "schema-pinning-enabled"
	RuleSchemaPinningBlock      = "schema-pinning-block"
	RuleNonemptyServerToolLists = "nonempty-server-tool-lists"
)

const WarningLiteralToolNameNotPattern = "literal-tool-name-not-pattern"

const literalToolWarningMessage = "Tool names are exact literals, not wildcard patterns; existence and authorization were not checked."

type Outcome string

const (
	OutcomePass          Outcome = "pass"
	OutcomeViolation     Outcome = "violation"
	OutcomeUnknown       Outcome = "unknown"
	OutcomeNotApplicable Outcome = "not_applicable"
)

type Status string

const (
	StatusAccepted      Status = "accepted"
	StatusRejected      Status = "rejected"
	StatusIndeterminate Status = "indeterminate"
	StatusError         Status = "error"
)

type Location struct {
	Source string `json:"source"`
	Path   string `json:"path,omitempty"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}

type Result struct {
	Rule        string   `json:"rule"`
	Outcome     Outcome  `json:"outcome"`
	Location    Location `json:"location"`
	ReasonCode  string   `json:"reason_code"`
	Message     string   `json:"message"`
	Remediation string   `json:"remediation,omitempty"`
}

type Warning struct {
	Code     string   `json:"code"`
	Location Location `json:"location"`
	Message  string   `json:"message"`
}

type Diagnostic struct {
	Code     string   `json:"code"`
	Location Location `json:"location"`
	Message  string   `json:"message"`
}

type Exclusion struct {
	Rule       string `json:"rule"`
	ReasonCode string `json:"reason_code"`
	Count      int    `json:"count"`
}

type Coverage struct {
	RulesSelected    int         `json:"rules_selected"`
	ApplicableChecks int         `json:"applicable_checks"`
	NotApplicable    int         `json:"not_applicable"`
	Exclusions       []Exclusion `json:"exclusions"`
}

type PolicyInfo struct {
	Version string   `json:"version"`
	Digest  string   `json:"digest"`
	Enabled []string `json:"enabled"`
}

type Report struct {
	SchemaVersion      int          `json:"schema_version"`
	CheckerVersion     string       `json:"checker_version"`
	Policy             PolicyInfo   `json:"policy"`
	Accepted           bool         `json:"accepted"`
	EvaluationComplete bool         `json:"evaluation_complete"`
	Status             Status       `json:"status"`
	Coverage           Coverage     `json:"coverage"`
	Results            []Result     `json:"results"`
	Warnings           []Warning    `json:"warnings"`
	Diagnostics        []Diagnostic `json:"diagnostics"`
}

func newReport() *Report {
	return &Report{
		SchemaVersion:  SchemaVersion,
		CheckerVersion: CheckerVersion,
		Policy: PolicyInfo{
			Enabled: []string{},
		},
		Coverage: Coverage{
			Exclusions: []Exclusion{},
		},
		Results:     []Result{},
		Warnings:    []Warning{},
		Diagnostics: []Diagnostic{},
	}
}

func (r *Report) addDiagnostic(code, source, path, message string) {
	r.Diagnostics = append(r.Diagnostics, Diagnostic{
		Code:     code,
		Location: Location{Source: source, Path: path},
		Message:  message,
	})
}

func (r *Report) finalize() {
	if r.Results == nil {
		r.Results = []Result{}
	}
	if r.Warnings == nil {
		r.Warnings = []Warning{}
	}
	if r.Diagnostics == nil {
		r.Diagnostics = []Diagnostic{}
	}
	if r.Policy.Enabled == nil {
		r.Policy.Enabled = []string{}
	}
	if r.Coverage.Exclusions == nil {
		r.Coverage.Exclusions = []Exclusion{}
	}
	sort.SliceStable(r.Coverage.Exclusions, func(i, j int) bool {
		if r.Coverage.Exclusions[i].Rule != r.Coverage.Exclusions[j].Rule {
			return r.Coverage.Exclusions[i].Rule < r.Coverage.Exclusions[j].Rule
		}
		return r.Coverage.Exclusions[i].ReasonCode < r.Coverage.Exclusions[j].ReasonCode
	})

	applicable := 0
	notApplicable := 0
	hasViolation := false
	hasUnknown := false
	for _, res := range r.Results {
		switch res.Outcome {
		case OutcomePass, OutcomeViolation, OutcomeUnknown:
			applicable++
			if res.Outcome == OutcomeViolation {
				hasViolation = true
			}
			if res.Outcome == OutcomeUnknown {
				hasUnknown = true
			}
		case OutcomeNotApplicable:
			notApplicable++
		}
	}
	r.Coverage.ApplicableChecks = applicable
	r.Coverage.NotApplicable = notApplicable
	r.Coverage.RulesSelected = len(r.Policy.Enabled)

	hasInputError := len(r.Diagnostics) > 0
	if hasInputError {
		r.Accepted = false
		r.Status = StatusError
		if !r.EvaluationComplete {
			return
		}
		return
	}
	if !r.EvaluationComplete {
		r.Accepted = false
		r.Status = StatusError
		return
	}
	if applicable == 0 {
		r.Accepted = false
		r.Status = StatusError
		r.addDiagnostic("no-applicable-checks", "entry", "", "Selected requirements produced no applicable checks.")
		return
	}
	if hasUnknown {
		r.Accepted = false
		r.Status = StatusIndeterminate
		return
	}
	if hasViolation {
		r.Accepted = false
		r.Status = StatusRejected
		return
	}
	r.Accepted = true
	r.Status = StatusAccepted
}

// ExitCode returns the validate-family exit: 0 accepted, 1 not accepted, 2
// accepted with warnings only.
func ExitCode(r *Report) int {
	if r == nil {
		return 1
	}
	if r.Accepted && len(r.Warnings) > 0 {
		return 2
	}
	if r.Accepted {
		return 0
	}
	return 1
}

// FormatJSON writes one versioned JSON document. It buffers first so a
// failed encode does not emit a partial report.
func FormatJSON(w io.Writer, r *Report) error {
	if r == nil {
		return fmt.Errorf("policy report is missing")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(r); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// FormatText writes the stable human report. Findings use aliases and
// structural paths rather than raw candidate values.
func FormatText(w io.Writer, r *Report) error {
	if r == nil {
		return fmt.Errorf("policy report is missing")
	}
	headline := "Declared-stack policy error"
	switch r.Status {
	case StatusAccepted:
		headline = "Declared-stack policy accepted"
	case StatusRejected:
		headline = "Declared-stack policy rejected"
	case StatusIndeterminate:
		headline = "Declared-stack policy indeterminate"
	}
	if _, err := fmt.Fprintln(w, headline); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  checker: %s\n", r.CheckerVersion); err != nil {
		return err
	}
	digest := r.Policy.Digest
	if digest == "" {
		digest = "none"
	}
	version := r.Policy.Version
	if version == "" {
		version = "none"
	}
	if _, err := fmt.Fprintf(w, "  policy: version %s digest %s\n", version, digest); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  selected_rules: %d\n", r.Coverage.RulesSelected); err != nil {
		return err
	}
	if len(r.Policy.Enabled) > 0 {
		if _, err := fmt.Fprintf(w, "  enabled: %s\n", strings.Join(r.Policy.Enabled, " ")); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "  applicable_checks: %d\n", r.Coverage.ApplicableChecks); err != nil {
		return err
	}
	for _, ex := range r.Coverage.Exclusions {
		label := sanitizeExclusionLabel(ex.ReasonCode)
		if ex.ReasonCode == reasonSourceBuilt {
			label = "source_built"
		}
		if _, err := fmt.Fprintf(w, "  excluded_%s: %d\n", label, ex.Count); err != nil {
			return err
		}
	}
	if len(r.Diagnostics) > 0 {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		for _, d := range r.Diagnostics {
			if _, err := fmt.Fprintf(w, "ERROR %s\n", d.Code); err != nil {
				return err
			}
			if loc := formatLocation(d.Location); loc != "" {
				if _, err := fmt.Fprintf(w, "  %s\n", loc); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(w, "  %s\n", d.Message); err != nil {
				return err
			}
		}
	}
	printedGap := len(r.Diagnostics) > 0
	for _, res := range r.Results {
		if res.Outcome == OutcomePass || res.Outcome == OutcomeNotApplicable {
			continue
		}
		if !printedGap {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
			printedGap = true
		} else {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "%s %s\n", strings.ToUpper(string(res.Outcome)), res.Rule); err != nil {
			return err
		}
		if loc := formatLocation(res.Location); loc != "" {
			if _, err := fmt.Fprintf(w, "  %s\n", loc); err != nil {
				return err
			}
		}
		if res.ReasonCode != "" {
			if _, err := fmt.Fprintf(w, "  reason: %s\n", res.ReasonCode); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "  %s\n", res.Message); err != nil {
			return err
		}
		if res.Remediation != "" {
			if _, err := fmt.Fprintf(w, "  %s\n", res.Remediation); err != nil {
				return err
			}
		}
	}
	for _, warn := range r.Warnings {
		if !printedGap {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
			printedGap = true
		} else {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "WARNING %s\n", warn.Code); err != nil {
			return err
		}
		if loc := formatLocation(warn.Location); loc != "" {
			if _, err := fmt.Fprintf(w, "  %s\n", loc); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "  %s\n", warn.Message); err != nil {
			return err
		}
	}
	return nil
}

func formatLocation(loc Location) string {
	if loc.Source == "" && loc.Path == "" && loc.Line == 0 {
		return ""
	}
	var b strings.Builder
	if loc.Source != "" {
		b.WriteString(loc.Source)
	}
	if loc.Path != "" {
		if b.Len() > 0 {
			b.WriteString(": ")
		}
		b.WriteString(loc.Path)
	}
	if loc.Line > 0 {
		b.WriteByte(':')
		b.WriteString(itoa(loc.Line))
		if loc.Column > 0 {
			b.WriteByte(':')
			b.WriteString(itoa(loc.Column))
		}
	}
	return b.String()
}

func sanitizeExclusionLabel(code string) string {
	out := strings.Builder{}
	for _, r := range code {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			out.WriteRune(r)
		}
	}
	s := out.String()
	if s == "" {
		return "other"
	}
	return s
}
