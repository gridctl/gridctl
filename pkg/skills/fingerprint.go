package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/gridctl/gridctl/pkg/registry"
)

// Fingerprint captures the behavioral identity of a skill.
type Fingerprint struct {
	ContentHash string   `json:"contentHash" yaml:"content_hash"`
	ToolsHash   string   `json:"toolsHash" yaml:"tools_hash"`
	Tools       []string `json:"tools,omitempty" yaml:"tools,omitempty"`
	WorkflowLen int      `json:"workflowSteps" yaml:"workflow_steps"`
}

// ComputeFingerprint generates a behavioral fingerprint for a skill.
func ComputeFingerprint(skill *registry.AgentSkill) *Fingerprint {
	fp := &Fingerprint{}

	// Content hash — SHA-256 of the full SKILL.md body
	h := sha256.Sum256([]byte(skill.Body))
	fp.ContentHash = hex.EncodeToString(h[:])

	// Extract tools from allowed_tools field
	if skill.AllowedTools != "" {
		tools := parseToolList(skill.AllowedTools)
		sort.Strings(tools)
		fp.Tools = tools
		th := sha256.Sum256([]byte(strings.Join(tools, ",")))
		fp.ToolsHash = hex.EncodeToString(th[:])
	}

	// Count workflow steps from body
	fp.WorkflowLen = countWorkflowSteps(skill.Body)

	return fp
}

// BehavioralChanges compares two fingerprints and returns human-readable changes.
func BehavioralChanges(old, new *Fingerprint) []string {
	if old == nil || new == nil {
		return nil
	}

	var changes []string

	if old.ToolsHash != new.ToolsHash {
		added, removed := diffStringSlices(old.Tools, new.Tools)
		if len(added) > 0 {
			changes = append(changes, fmt.Sprintf("tools added: %s", strings.Join(added, ", ")))
		}
		if len(removed) > 0 {
			changes = append(changes, fmt.Sprintf("tools removed: %s", strings.Join(removed, ", ")))
		}
	}

	if old.WorkflowLen != new.WorkflowLen {
		changes = append(changes, fmt.Sprintf("workflow steps: %d → %d", old.WorkflowLen, new.WorkflowLen))
	}

	if old.ContentHash != new.ContentHash && len(changes) == 0 {
		changes = append(changes, "content changed")
	}

	return changes
}

// parseToolList splits a comma-separated tool list.
func parseToolList(s string) []string {
	var tools []string
	for _, t := range strings.Split(s, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			tools = append(tools, t)
		}
	}
	return tools
}

// countWorkflowSteps counts lines that look like workflow step definitions.
func countWorkflowSteps(body string) int {
	count := 0
	inWorkflow := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## Workflow") || strings.HasPrefix(trimmed, "## workflow") {
			inWorkflow = true
			continue
		}
		if inWorkflow && strings.HasPrefix(trimmed, "## ") {
			inWorkflow = false
		}
		if inWorkflow && strings.HasPrefix(trimmed, "- ") {
			count++
		}
	}
	return count
}

// diffStringSlices returns added and removed items between old and new slices.
func diffStringSlices(old, new []string) (added, removed []string) {
	oldSet := make(map[string]bool, len(old))
	for _, s := range old {
		oldSet[s] = true
	}
	newSet := make(map[string]bool, len(new))
	for _, s := range new {
		newSet[s] = true
	}

	for _, s := range new {
		if !oldSet[s] {
			added = append(added, s)
		}
	}
	for _, s := range old {
		if !newSet[s] {
			removed = append(removed, s)
		}
	}
	return
}
