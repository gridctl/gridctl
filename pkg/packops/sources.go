package packops

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"

	gitpkg "github.com/gridctl/gridctl/pkg/git"
	"github.com/gridctl/gridctl/pkg/pack"
	"github.com/gridctl/gridctl/pkg/skills"
)

// cloneAndDiscover clones a repository. Tests replace it to observe auth.
var cloneAndDiscover = skills.CloneAndDiscover

// beforeExternalImport, when set, runs after clones and before each external
// import's commit re-check. Tests mutate the cache worktree here.
var beforeExternalImport func(name string, clone *skills.CloneResult)

// resolvedSource is one declared external source after clone and selection.
type resolvedSource struct {
	spec           pack.Source
	clone          *skills.CloneResult
	auth           skills.AuthConfig
	sha            string
	skills         []string
	agents         []string
	importedSkills []string
	importedAgents []string
	err            error
}

// SourceSummary is one external source in an add, preview, or status document.
type SourceSummary struct {
	Name      string   `json:"name"`
	Repo      string   `json:"repo"`
	Ref       string   `json:"ref,omitempty"`
	CommitSHA string   `json:"commit_sha,omitempty"`
	Skills    []string `json:"skills"`
	Agents    []string `json:"agents"`
	Error     string   `json:"error,omitempty"`
}

func cloneDeclaredSources(ctx context.Context, manifest *pack.Manifest, opts AddOptions, lockPath string) (map[string]*resolvedSource, []string, error) {
	names := slices.Sorted(maps.Keys(manifest.Sources))
	if len(names) == 0 {
		return nil, nil, nil
	}
	lf, err := skills.ReadLockFile(lockPath)
	if err != nil {
		return nil, nil, err
	}
	var warnings []string
	out := make(map[string]*resolvedSource, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		spec := manifest.Sources[name]
		rs := &resolvedSource{spec: spec}
		var override *skills.AuthConfig
		if opts.SourceAuth != nil {
			if cfg, ok := opts.SourceAuth[name]; ok {
				override = &cfg
			}
		}
		auth, aerr := resolveSourceAuth(manifest.Name, name, spec, override, opts.Auth, lf, opts.Resolver)
		if aerr != nil {
			rs.err = aerr
			warnings = append(warnings, sourceWarning(name, aerr))
			out[name] = rs
			continue
		}
		rs.auth = auth
		clone, cerr := cloneAndDiscover(spec.Repo, spec.Ref, spec.Path, auth, slog.Default())
		if cerr != nil {
			rs.err = cerr
			warnings = append(warnings, sourceWarning(name, cerr))
			out[name] = rs
			continue
		}
		if _, statErr := os.Stat(filepath.Join(clone.RepoPath, pack.ManifestFileName)); statErr == nil {
			rs.err = fmt.Errorf("source %q is itself a pack; packs cannot depend on packs", name)
			warnings = append(warnings, sourceWarning(name, rs.err))
			out[name] = rs
			continue
		}
		rs.clone = clone
		rs.sha = clone.CommitSHA
		out[name] = rs
	}
	return out, warnings, nil
}

func sourceWarning(name string, err error) string {
	return fmt.Sprintf("source %q: %s", name, redactSourceErr(err))
}

func redactSourceErr(err error) string {
	if err == nil {
		return ""
	}
	return gitpkg.RedactError(gitpkg.ClassifyError(err)).Error()
}

func unknownSourceAuthWarnings(manifest *pack.Manifest, auth map[string]skills.AuthConfig) []string {
	var names []string
	for name := range auth {
		if _, ok := manifest.Sources[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	var out []string
	for _, name := range names {
		out = append(out, fmt.Sprintf("source-auth %q does not match a declared source", name))
	}
	return out
}

func guardPackName(lockPath, name, repo string) error {
	lf, err := skills.ReadLockFile(lockPath)
	if err != nil {
		return err
	}
	existing, err := findPack(lf, name)
	if err != nil {
		if errors.Is(err, ErrNotImported) {
			return nil
		}
		return err
	}
	if existing.Source.Repo != repo {
		return &packError{
			reason: ErrNameCollision,
			msg:    fmt.Sprintf("pack name %q is already imported from %s; remove it before adding %s", name, existing.Source.Repo, repo),
		}
	}
	return nil
}

func recheckSourceCommit(rs *resolvedSource) error {
	if rs == nil || rs.clone == nil || rs.sha == "" {
		return fmt.Errorf("source has no recorded commit")
	}
	head, err := gitpkg.HeadCommit(rs.clone.RepoPath)
	if err != nil {
		return err
	}
	if head == rs.sha {
		return nil
	}
	recloned, err := cloneAndDiscover(rs.spec.Repo, rs.sha, rs.spec.Path, rs.auth, slog.Default())
	if err != nil {
		return fmt.Errorf("re-clone at %s failed: %w", skills.ShortSHA(rs.sha), err)
	}
	head, err = gitpkg.HeadCommit(recloned.RepoPath)
	if err != nil {
		return err
	}
	if head != rs.sha {
		return fmt.Errorf("head %s does not match recorded %s", skills.ShortSHA(head), skills.ShortSHA(rs.sha))
	}
	rs.clone = recloned
	return nil
}

func failSource(resolved *resolvedSelection, name string, err error) {
	rs := resolved.sources[name]
	if rs == nil {
		return
	}
	rs.err = err
	rs.importedSkills = nil
	rs.importedAgents = nil
	detail := redactSourceErr(err)
	if resolved.unresolvedDetails == nil {
		resolved.unresolvedDetails = map[string]string{}
	}
	for _, skillName := range rs.skills {
		token := "skill/" + skillName
		resolved.unresolved = append(resolved.unresolved, token)
		resolved.unresolvedDetails[token] = detail
		resolved.skills = removeName(resolved.skills, skillName)
	}
	for _, agentName := range rs.agents {
		token := "agent/" + agentName
		resolved.unresolved = append(resolved.unresolved, token)
		resolved.unresolvedDetails[token] = detail
		resolved.agents = removeName(resolved.agents, agentName)
	}
	rs.skills = nil
	rs.agents = nil
}

func removeName(names []string, drop string) []string {
	if len(names) == 0 {
		return names
	}
	out := names[:0]
	for _, name := range names {
		if name != drop {
			out = append(out, name)
		}
	}
	return out
}

func nonNilNames(names []string) []string {
	if names == nil {
		return []string{}
	}
	return names
}

func sourceSummaries(resolved resolvedSelection, useImported bool) []SourceSummary {
	if len(resolved.sources) == 0 {
		return nil
	}
	out := make([]SourceSummary, 0, len(resolved.sources))
	for _, name := range slices.Sorted(maps.Keys(resolved.sources)) {
		rs := resolved.sources[name]
		item := SourceSummary{
			Name:      name,
			Repo:      redactRepo(rs.spec.Repo),
			Ref:       rs.spec.Ref,
			CommitSHA: rs.sha,
			Skills:    []string{},
			Agents:    []string{},
		}
		if rs.err != nil {
			item.Error = redactSourceErr(rs.err)
		} else if useImported {
			item.Skills = orEmpty(rs.importedSkills)
			item.Agents = orEmpty(rs.importedAgents)
		} else {
			item.Skills = orEmpty(rs.skills)
			item.Agents = orEmpty(rs.agents)
		}
		out = append(out, item)
	}
	return out
}

func memberKey(packName, sourceName string) string {
	return packName + "/" + sourceName
}

func redactRepo(repo string) string {
	return gitpkg.RedactURL(repo)
}
