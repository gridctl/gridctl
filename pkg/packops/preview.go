package packops

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/gridctl/gridctl/pkg/pack"
	"github.com/gridctl/gridctl/pkg/skills"
)

// PreviewOptions parameterizes a read-only pack resolution.
type PreviewOptions struct {
	Repo string
	Ref  string
	Path string
	// Auth authenticates the clone. A zero value keeps the ambient
	// behavior (ssh-agent for SSH, GITHUB_TOKEN for HTTPS, else anonymous).
	Auth skills.AuthConfig
}

// PreviewResource is one resolved resource with its scan findings.
type PreviewResource struct {
	Kind     string                   `json:"kind"`
	Name     string                   `json:"name"`
	Findings []skills.SecurityFinding `json:"findings,omitempty"`
	// Blocking mirrors the import gate exactly: body findings always
	// block; supporting-file findings block only at danger severity.
	// Non-blocking findings stay visible without forcing a trust grant.
	Blocking bool `json:"blocking,omitempty"`
}

// PreviewResult is a pack manifest resolved against its repository,
// with nothing written: what an import would select, name by name, and
// which resources carry security findings.
type PreviewResult struct {
	Pack        string            `json:"pack"`
	Version     string            `json:"version,omitempty"`
	Description string            `json:"description,omitempty"`
	Author      string            `json:"author,omitempty"`
	Wiring      bool              `json:"wiring"`
	Clients     []string          `json:"clients,omitempty"`
	Skills      []PreviewResource `json:"skills"`
	Agents      []PreviewResource `json:"agents"`
	Rules       []PreviewResource `json:"rules"`
	Unresolved  []string          `json:"unresolved,omitempty"`
	Warnings    []string          `json:"warnings,omitempty"`
}

// FindingsError blocks an import whose resolved selection carries
// security findings and no trust acknowledgment. It is returned before
// any write, so a refusal never follows a half-done import.
type FindingsError struct {
	Pack      string
	Resources []PreviewResource
}

func (e *FindingsError) Error() string {
	names := make([]string, 0, len(e.Resources))
	for _, r := range e.Resources {
		names = append(names, r.Kind+"/"+r.Name)
	}
	return fmt.Sprintf("pack %q has security findings on %s; re-run with trust to accept them", e.Pack, strings.Join(names, ", "))
}

// Preview clones the repository, parses the pack manifest, and resolves
// the selection with scan findings, writing nothing.
func Preview(ctx context.Context, opts PreviewOptions) (*PreviewResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	clone, err := skills.CloneAndDiscover(opts.Repo, opts.Ref, opts.Path, opts.Auth, slog.Default())
	if err != nil {
		return nil, err
	}
	manifest, err := pack.ParseFile(filepath.Join(clone.RepoPath, pack.ManifestFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &packError{
				reason: ErrNoManifest,
				msg:    fmt.Sprintf("no %s found at the repository root; this repository is not a pack (import it with the Skill flow, or add a manifest)", pack.ManifestFileName),
			}
		}
		return nil, err
	}
	discoveredRules := discoverPackRules(clone.RepoPath)
	resolved := resolvePackSelection(manifest, clone, discoveredRules)

	res := &PreviewResult{
		Pack:        manifest.Name,
		Version:     manifest.Version,
		Description: manifest.Description,
		Author:      manifest.Author.Name,
		Wiring:      manifest.Wiring,
		Clients:     manifest.Clients,
		Skills:      []PreviewResource{},
		Agents:      []PreviewResource{},
		Rules:       []PreviewResource{},
		Unresolved:  resolved.unresolved,
		Warnings:    manifest.Warnings(),
	}
	for _, pr := range scanResources(clone, resolved, discoveredRules, false) {
		switch pr.Kind {
		case "skill":
			res.Skills = append(res.Skills, pr)
		case "agent":
			res.Agents = append(res.Agents, pr)
		case "rule":
			res.Rules = append(res.Rules, pr)
		}
	}
	return res, nil
}

// scanSelection returns only the resources whose findings would block
// the import gate; a refusal built from it covers exactly what the
// importer would skip.
func scanSelection(clone *skills.CloneResult, resolved resolvedSelection, discoveredRules map[string]PackRuleFile) []PreviewResource {
	var out []PreviewResource
	for _, pr := range scanResources(clone, resolved, discoveredRules, true) {
		if pr.Blocking {
			out = append(out, pr)
		}
	}
	return out
}

// scanResources scans every resolved resource. With flaggedOnly, clean
// resources are dropped; otherwise every resource appears, findings or
// not.
func scanResources(clone *skills.CloneResult, resolved resolvedSelection, discoveredRules map[string]PackRuleFile, flaggedOnly bool) []PreviewResource {
	var out []PreviewResource
	keep := func(pr PreviewResource) {
		if !flaggedOnly || len(pr.Findings) > 0 {
			out = append(out, pr)
		}
	}
	for _, name := range resolved.skills {
		pr := PreviewResource{Kind: "skill", Name: name}
		for _, ds := range clone.Skills {
			if ds.Name == name && ds.Skill != nil {
				// Body plus supporting files: the same gate the importer
				// applies, so a clean SKILL.md over a dangerous script
				// cannot slip past a refuse-before-import caller.
				findings, blocking := skills.ScanSkillTree(ds.Skill, filepath.Join(clone.RepoPath, ds.Path))
				pr.Findings = findings
				pr.Blocking = blocking
				break
			}
		}
		keep(pr)
	}
	for _, name := range resolved.agents {
		pr := PreviewResource{Kind: "agent", Name: name}
		for _, da := range clone.Agents {
			if da.Name == name && da.Definition != nil {
				if scan := skills.ScanAgent(da.Definition); !scan.Safe {
					pr.Findings = scan.Findings
					pr.Blocking = true
				}
				break
			}
		}
		keep(pr)
	}
	for _, name := range resolved.rules {
		pr := PreviewResource{Kind: "rule", Name: name}
		if rf, ok := discoveredRules[name]; ok {
			if data, err := os.ReadFile(rf.Path); err == nil { // #nosec G304 -- path from pack clone discovery
				if scan := skills.ScanFragment(name, data); !scan.Safe {
					pr.Findings = scan.Findings
					pr.Blocking = true
				}
			}
		}
		keep(pr)
	}
	return out
}
