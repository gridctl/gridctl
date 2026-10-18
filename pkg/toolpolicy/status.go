package toolpolicy

import "github.com/gridctl/gridctl/pkg/mcp"

// RuleStatus is one authored rule, index 0-based as written.
type RuleStatus struct {
	Index  int    `json:"index"`
	Match  string `json:"match"`
	Action string `json:"action"`
}

// ToolStatus is the verdict dispatch would apply if the tool were exposed.
// A hidden tool is still evaluated; dispatch itself rejects hidden names
// before any gate. RuleIndex is omitted unless a rule matched, so index 0
// is not dropped.
type ToolStatus struct {
	Action    string `json:"action"`
	Source    string `json:"source"`
	RuleIndex *int   `json:"rule_index,omitempty"`
	Rule      string `json:"rule,omitempty"`
	Exposed   bool   `json:"exposed"`
}

// CatalogEntry is one pre-whitelist catalog tool plus whether the
// post-whitelist dispatch index currently exposes it.
type CatalogEntry struct {
	Name        string
	Annotations *mcp.ToolAnnotations
	Exposed     bool
}

// StatusReport is the GET /api/policy payload. Rules and Tools are never
// nil. Default and BlockDestructive are omitted when the block is absent
// so the unconfigured body is {"configured":false,"rules":[],"tools":{}}.
type StatusReport struct {
	Configured       bool                  `json:"configured"`
	Default          string                `json:"default,omitempty"`
	BlockDestructive *bool                 `json:"block_destructive,omitempty"`
	Rules            []RuleStatus          `json:"rules"`
	Tools            map[string]ToolStatus `json:"tools"`
}

// EmptyStatus is the unconfigured payload. Never null collections.
func EmptyStatus() StatusReport {
	return StatusReport{Rules: []RuleStatus{}, Tools: map[string]ToolStatus{}}
}

// Status snapshots the authored block with no per-tool rows. A nil policy
// reports configured false.
func (p *Policy) Status() StatusReport {
	return p.StatusWithCatalog(nil)
}

// StatusWithCatalog snapshots the block and evaluates each catalog entry
// with Evaluate, using that entry's own annotations. A nil policy ignores
// the catalog and returns the unconfigured payload: a hidden tool's row
// is only meaningful once a block is declared.
func (p *Policy) StatusWithCatalog(entries []CatalogEntry) StatusReport {
	if p == nil {
		return EmptyStatus()
	}
	report := StatusReport{
		Configured:       true,
		Default:          p.authoredDefault,
		BlockDestructive: boolPtr(p.blockDestructive),
		Rules:            make([]RuleStatus, 0, len(p.rules)),
		Tools:            map[string]ToolStatus{},
	}
	for _, rule := range p.rules {
		report.Rules = append(report.Rules, RuleStatus{
			Index:  rule.index,
			Match:  rule.pattern,
			Action: rule.action,
		})
	}
	for _, entry := range entries {
		if entry.Name == "" {
			continue
		}
		verdict := p.Evaluate(entry.Name, entry.Annotations)
		row := ToolStatus{
			Action:  verdict.Action,
			Source:  verdict.Source,
			Rule:    verdict.Rule,
			Exposed: entry.Exposed,
		}
		if verdict.Matched {
			idx := verdict.RuleIndex
			row.RuleIndex = &idx
		}
		report.Tools[entry.Name] = row
	}
	return report
}

func boolPtr(v bool) *bool { return &v }
