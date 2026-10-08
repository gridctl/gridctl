package importer

import (
	"fmt"
	"regexp"
	"strings"
)

// projectTokenRe matches one ${...} token. Substitution replaces only the
// workspace and Claude project-directory forms. Other tokens stay in place
// so the unsupported-placeholder check can see them.
var projectTokenRe = regexp.MustCompile(`\$\{[^}]+\}`)

var unsupportedPlaceholders = []struct {
	needle string
	label  string
}{
	{"${input:", "${input:...}"},
	{"${userHome}", "${userHome}"},
	{"${workspaceFolderBasename}", "${workspaceFolderBasename}"},
	{"${pathSeparator}", "${pathSeparator}"},
	{"${/}", "${/}"},
	{"${CLAUDE_PLUGIN_ROOT}", "${CLAUDE_PLUGIN_ROOT}"},
	{"${CLAUDE_PLUGIN_DATA}", "${CLAUDE_PLUGIN_DATA}"},
}

// mapState records placeholder names substituted in one entry. A nil state
// leaves values unchanged.
type mapState struct {
	ownerDir string
	names    []string
}

func (s *mapState) note(name string) {
	if s == nil {
		return
	}
	for _, existing := range s.names {
		if existing == name {
			return
		}
	}
	s.names = append(s.names, name)
}

func (s *mapState) warnings() []string {
	if s == nil || len(s.names) == 0 {
		return nil
	}
	out := make([]string, len(s.names))
	for i, name := range s.names {
		out[i] = fmt.Sprintf("resolved %s to %s; the imported entry is machine-specific", name, s.ownerDir)
	}
	return out
}

func (s *mapState) apply(v string) (string, error) {
	if s == nil || s.ownerDir == "" {
		return v, nil
	}
	next := projectTokenRe.ReplaceAllStringFunc(v, func(token string) string {
		switch {
		case token == "${workspaceFolder}": // #nosec G101 -- placeholder name, not a credential
			s.note("${workspaceFolder}")
			return s.ownerDir
		case token == "${CLAUDE_PROJECT_DIR}" || strings.HasPrefix(token, "${CLAUDE_PROJECT_DIR:-"):
			s.note("${CLAUDE_PROJECT_DIR}")
			return s.ownerDir
		default:
			return token
		}
	})
	if label, ok := unsupportedLabel(next); ok {
		return "", &MapError{
			Reason: SkipNativeExpression,
			Detail: label + " in command, args, url, env, or headers is not imported; replace it with a literal value",
		}
	}
	return next, nil
}

func (s *mapState) substituteArgv(parts []string) ([]string, error) {
	if s == nil || s.ownerDir == "" || len(parts) == 0 {
		return parts, nil
	}
	out := make([]string, len(parts))
	for i, part := range parts {
		next, err := s.apply(part)
		if err != nil {
			return nil, err
		}
		out[i] = next
	}
	return out, nil
}

func unsupportedLabel(v string) (string, bool) {
	for _, item := range unsupportedPlaceholders {
		if strings.Contains(v, item.needle) {
			return item.label, true
		}
	}
	return "", false
}

func nonOpenCodeCwd(raw map[string]any, scope string) (string, error) {
	v, ok := raw["cwd"]
	if !ok || v == nil {
		return "", nil
	}
	cwd, ok := v.(string)
	if !ok || strings.TrimSpace(cwd) == "" {
		return "", nil
	}
	switch scope {
	case "project", "local":
		return "", &MapError{Reason: SkipUntransferredOption, Detail: "working directory cannot be transferred to the stack"}
	case "user":
		return "cwd was not transferred; relative paths in the command may not resolve", nil
	default:
		return "", nil
	}
}

func substituteMappedFields(raw map[string]any, st *mapState) error {
	for _, key := range []string{"httpUrl", "url", "serverUrl", "uri"} {
		if err := substituteRawString(raw, key, st); err != nil {
			return err
		}
	}
	for _, key := range []string{"env", "envs", "environment", "headers"} {
		if err := substituteStringMap(raw, key, st); err != nil {
			return err
		}
	}
	return substituteArgs(raw, st)
}

func substituteRawString(raw map[string]any, key string, st *mapState) error {
	v, ok := raw[key].(string)
	if !ok || v == "" {
		return nil
	}
	next, err := st.apply(v)
	if err != nil {
		return err
	}
	raw[key] = next
	return nil
}

func substituteStringMap(raw map[string]any, key string, st *mapState) error {
	src, ok := raw[key].(map[string]any)
	if !ok {
		return nil
	}
	for name, value := range src {
		s, ok := value.(string)
		if !ok {
			continue
		}
		next, err := st.apply(s)
		if err != nil {
			return err
		}
		src[name] = next
	}
	return nil
}

func substituteArgs(raw map[string]any, st *mapState) error {
	args, ok := raw["args"].([]any)
	if !ok {
		return nil
	}
	for i, item := range args {
		s, ok := item.(string)
		if !ok {
			continue
		}
		next, err := st.apply(s)
		if err != nil {
			return err
		}
		args[i] = next
	}
	return nil
}

func cloneEntryMap(raw map[string]any) map[string]any {
	if raw == nil {
		return nil
	}
	out := make(map[string]any, len(raw))
	for k, v := range raw {
		switch val := v.(type) {
		case map[string]any:
			inner := make(map[string]any, len(val))
			for ik, iv := range val {
				inner[ik] = iv
			}
			out[k] = inner
		case []any:
			cp := make([]any, len(val))
			copy(cp, val)
			out[k] = cp
		default:
			out[k] = v
		}
	}
	return out
}
