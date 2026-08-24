package modelsync

import (
	"fmt"
	"sort"
)

// Starter templates keyed by --template name. Each is a complete,
// commented policy for one of the three common topologies. Backends are
// placeholders: they must match model_name values in the user's own
// LiteLLM model_list, which the fragment references but never re-emits.
var templates = map[string]string{
	"local-only": `# gridctl models policy: everything served by local models.
# Backends reference model_name values from YOUR LiteLLM config.yaml
# model_list; gridctl only renders the router that picks between them.
name: default
kind: models
description: Route all tiers to local models

router:
  entry_model: smart-router     # what clients select
  default_tier: MEDIUM          # unclassifiable requests land here

backends:
  - qwen-local

tiers:
  SIMPLE: qwen-local
  MEDIUM: qwen-local
  COMPLEX: qwen-local
  REASONING: qwen-local

# Coding agents carry huge system prompts; ignore raw token count.
weights:
  tokenCount: 0.0
  reasoningMarkers: 0.40
  technicalTerms: 0.25
  codePresence: 0.20
  simpleIndicators: 0.10
  multiStepPatterns: 0.05

# Auto-router keys gridctl does not model ride through verbatim, e.g.:
# passthrough:
#   session_affinity: true

clients:
  opencode:
    provider_id: litellm
    base_url: http://localhost:4000/v1
    api_key_env: LITELLM_KEY
    schema: detect

targets:
  litellm:
    config_path: ~/.litellm/config.yaml
`,
	"hybrid": `# gridctl models policy: local models for routine work, cloud for the
# hard tiers. Backends reference model_name values from YOUR LiteLLM
# config.yaml model_list; gridctl only renders the router.
name: default
kind: models
description: Local for routine tiers, cloud for complex work

router:
  entry_model: smart-router
  default_tier: MEDIUM

backends:
  - qwen-local
  - claude-sonnet

tiers:
  SIMPLE: qwen-local
  MEDIUM: qwen-local
  COMPLEX: claude-sonnet
  REASONING: claude-sonnet

weights:
  tokenCount: 0.0
  reasoningMarkers: 0.40
  technicalTerms: 0.25
  codePresence: 0.20
  simpleIndicators: 0.10
  multiStepPatterns: 0.05

clients:
  opencode:
    provider_id: litellm
    base_url: http://localhost:4000/v1
    api_key_env: LITELLM_KEY
    schema: detect

targets:
  litellm:
    config_path: ~/.litellm/config.yaml
`,
	"cloud-primary": `# gridctl models policy: cloud models first, a local model for cheap
# background work. Backends reference model_name values from YOUR
# LiteLLM config.yaml model_list; gridctl only renders the router.
name: default
kind: models
description: Cloud-primary with a local background tier

router:
  entry_model: smart-router
  default_tier: COMPLEX

backends:
  - qwen-local
  - claude-sonnet
  - claude-opus

tiers:
  SIMPLE: qwen-local
  MEDIUM: claude-sonnet
  COMPLEX: claude-sonnet
  REASONING: claude-opus

weights:
  tokenCount: 0.0
  reasoningMarkers: 0.40
  technicalTerms: 0.25
  codePresence: 0.20
  simpleIndicators: 0.10
  multiStepPatterns: 0.05

clients:
  opencode:
    provider_id: litellm
    base_url: http://localhost:4000/v1
    api_key_env: LITELLM_KEY
    schema: detect

targets:
  litellm:
    config_path: ~/.litellm/config.yaml
`,
}

// DefaultTemplate is the scaffold used when init names no template.
const DefaultTemplate = "hybrid"

// TemplateNames lists the available --template values, sorted.
func TemplateNames() []string {
	names := make([]string, 0, len(templates))
	for name := range templates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// InitFromTemplate scaffolds the policy document from a named starter.
func (m *Manager) InitFromTemplate(name string, force bool) error {
	if name == "" {
		name = DefaultTemplate
	}
	tmpl, ok := templates[name]
	if !ok {
		return fmt.Errorf("unknown template %q (available: %v)", name, TemplateNames())
	}
	return m.initWith(tmpl, force)
}
