package skills

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/stretchr/testify/assert"
)

func TestComputeFingerprint_Basic(t *testing.T) {
	skill := &registry.AgentSkill{
		Name:         "test-skill",
		AllowedTools: "tool-a, tool-b, tool-c",
		Body: `# Test Skill

## Workflow
- Step 1: do something
- Step 2: do another thing

## Notes
Some notes here.
`,
	}

	fp := ComputeFingerprint(skill)

	assert.NotEmpty(t, fp.ContentHash)
	assert.NotEmpty(t, fp.ToolsHash)
	assert.Equal(t, []string{"tool-a", "tool-b", "tool-c"}, fp.Tools)
	assert.Equal(t, 2, fp.WorkflowLen)
}

func TestComputeFingerprint_NoTools(t *testing.T) {
	skill := &registry.AgentSkill{
		Name: "no-tools",
		Body: "Just some content",
	}

	fp := ComputeFingerprint(skill)

	assert.NotEmpty(t, fp.ContentHash)
	assert.Empty(t, fp.ToolsHash)
	assert.Nil(t, fp.Tools)
	assert.Equal(t, 0, fp.WorkflowLen)
}

func TestComputeFingerprint_Deterministic(t *testing.T) {
	skill := &registry.AgentSkill{
		Name:         "deterministic",
		AllowedTools: "tool-b, tool-a",
		Body:         "content",
	}

	fp1 := ComputeFingerprint(skill)
	fp2 := ComputeFingerprint(skill)

	assert.Equal(t, fp1.ContentHash, fp2.ContentHash)
	assert.Equal(t, fp1.ToolsHash, fp2.ToolsHash)
	// Tools should be sorted
	assert.Equal(t, []string{"tool-a", "tool-b"}, fp1.Tools)
}

func TestBehavioralChanges_ToolAdded(t *testing.T) {
	old := &Fingerprint{
		ContentHash: "aaa",
		ToolsHash:   "bbb",
		Tools:       []string{"tool-a"},
		WorkflowLen: 2,
	}
	new := &Fingerprint{
		ContentHash: "ccc",
		ToolsHash:   "ddd",
		Tools:       []string{"tool-a", "tool-b"},
		WorkflowLen: 2,
	}

	changes := BehavioralChanges(old, new)

	assert.Contains(t, changes, "tools added: tool-b")
}

func TestBehavioralChanges_ToolRemoved(t *testing.T) {
	old := &Fingerprint{
		ContentHash: "aaa",
		ToolsHash:   "bbb",
		Tools:       []string{"tool-a", "tool-b"},
		WorkflowLen: 2,
	}
	new := &Fingerprint{
		ContentHash: "ccc",
		ToolsHash:   "ddd",
		Tools:       []string{"tool-a"},
		WorkflowLen: 2,
	}

	changes := BehavioralChanges(old, new)

	assert.Contains(t, changes, "tools removed: tool-b")
}

func TestBehavioralChanges_WorkflowChanged(t *testing.T) {
	old := &Fingerprint{
		ContentHash: "aaa",
		ToolsHash:   "same",
		Tools:       []string{"tool-a"},
		WorkflowLen: 2,
	}
	new := &Fingerprint{
		ContentHash: "bbb",
		ToolsHash:   "same",
		Tools:       []string{"tool-a"},
		WorkflowLen: 5,
	}

	changes := BehavioralChanges(old, new)

	assert.Contains(t, changes, "workflow steps: 2 → 5")
}

func TestBehavioralChanges_ContentOnlyChange(t *testing.T) {
	old := &Fingerprint{
		ContentHash: "aaa",
		ToolsHash:   "same",
		Tools:       []string{"tool-a"},
		WorkflowLen: 2,
	}
	new := &Fingerprint{
		ContentHash: "bbb",
		ToolsHash:   "same",
		Tools:       []string{"tool-a"},
		WorkflowLen: 2,
	}

	changes := BehavioralChanges(old, new)

	assert.Contains(t, changes, "content changed")
}

func TestBehavioralChanges_NoChange(t *testing.T) {
	fp := &Fingerprint{
		ContentHash: "same",
		ToolsHash:   "same",
		Tools:       []string{"tool-a"},
		WorkflowLen: 2,
	}

	changes := BehavioralChanges(fp, fp)

	assert.Empty(t, changes)
}

func TestBehavioralChanges_NilInputs(t *testing.T) {
	assert.Nil(t, BehavioralChanges(nil, nil))
	assert.Nil(t, BehavioralChanges(&Fingerprint{}, nil))
	assert.Nil(t, BehavioralChanges(nil, &Fingerprint{}))
}
