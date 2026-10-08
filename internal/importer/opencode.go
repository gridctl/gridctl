package importer

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gridctl/gridctl/pkg/config"
)

// envNamePattern is the identifier grammar in pkg/config/expand.go. A native
// {env:NAME} is converted to ${NAME} only when NAME matches it, so stack load
// can expand the reference. Invalid names are not rewritten to a literal that
// would never expand.
var envNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// nativeExprPattern finds OpenCode {env:...} and {file:...} tokens. OpenCode
// substitutes these in config text before parse; import must not.
var nativeExprPattern = regexp.MustCompile(`\{(env|file):([^{}]*)\}`)

// openCodeNative reports whether an OpenCode entry uses a shape the string
// dialect does not represent: a non-string command, or the native environment
// map. A string command without environment stays on the legacy path.
func openCodeNative(raw map[string]any) bool {
	if _, ok := raw["environment"]; ok {
		return true
	}
	cmd, ok := raw["command"]
	if !ok {
		return false
	}
	_, isString := cmd.(string)
	return !isString
}

// mapOpenCodeNative maps a newly supported OpenCode local entry. Native
// command arrays are argv: they are not shell-split, and they are not passed
// through unwrapCmdC or unwrapMCPRemote. Those helpers stay on the legacy
// string path, including IsGatewaySelfEntry.
func mapOpenCodeNative(server config.MCPServer, warnings []string, raw map[string]any, st *mapState) (config.MCPServer, []string, error) {
	cmd, hasCmd := raw["command"]
	_, cmdIsString := cmd.(string)
	nativeArray := hasCmd && !cmdIsString

	if nativeArray {
		optWarnings, err := openCodeLocalOptions(raw)
		if err != nil {
			return server, warnings, err
		}
		warnings = append(warnings, optWarnings...)
		argv, err := openCodeArgv(raw, st)
		if err != nil {
			return server, warnings, err
		}
		env, err := openCodeEnvironment(raw, true)
		if err != nil {
			return server, warnings, err
		}
		server.Command = argv
		server.Transport = "stdio"
		server.Env = env
		return server, warnings, nil
	}

	env, err := openCodeEnvironment(raw, false)
	if err != nil {
		return server, warnings, err
	}
	if !hasCmd {
		return server, warnings, &MapError{Reason: SkipUnsupported, Detail: "environment is set but the entry has no command"}
	}
	command := commandSlice(raw)
	if st != nil {
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
		if len(env) > 0 {
			warnings = append(warnings, "native environment was not transferred onto the converted bridge URL")
		}
		return server, warnings, nil
	}
	server.Command = command
	server.Transport = "stdio"
	server.Env = env
	return server, warnings, nil
}

// openCodeArgv validates a native command array and preserves every element
// exactly, including empty arguments, spaces, quotes, and backslashes.
func openCodeArgv(raw map[string]any, st *mapState) ([]string, error) {
	cmd, ok := raw["command"]
	if !ok {
		return nil, &MapError{Reason: SkipUnsupported, Detail: "entry has neither a command nor a URL"}
	}
	if cmd == nil {
		return nil, &MapError{Reason: SkipUnsupported, Detail: "command is null"}
	}
	arr, ok := cmd.([]any)
	if !ok {
		return nil, &MapError{Reason: SkipUnsupported, Detail: "command must be a string or an array of strings"}
	}
	if _, hasArgs := raw["args"]; hasArgs {
		return nil, &MapError{Reason: SkipAmbiguous, Detail: "native command array cannot be combined with args; remove one of them"}
	}
	if len(arr) == 0 {
		return nil, &MapError{Reason: SkipUnsupported, Detail: "command array is empty"}
	}
	out := make([]string, len(arr))
	for i, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, &MapError{Reason: SkipUnsupported, Detail: "command array must contain only strings"}
		}
		next := s
		if st != nil {
			var err error
			next, err = st.apply(s)
			if err != nil {
				return nil, err
			}
		}
		next, err := translateNativeRef(next)
		if err != nil {
			return nil, err
		}
		out[i] = next
	}
	if strings.TrimSpace(out[0]) == "" {
		return nil, &MapError{Reason: SkipUnsupported, Detail: "command executable is blank"}
	}
	return out, nil
}

// openCodeExecutionConstraints skips an OpenCode entry whose enabled flag
// or working directory cannot be represented. It applies to every OpenCode
// shape, including remote entries and string commands. Dropping either
// would change execution. No stack fields are added, and the detail carries
// no config value.
func openCodeExecutionConstraints(raw map[string]any) error {
	if v, ok := raw["enabled"]; ok && v != nil {
		enabled, ok := v.(bool)
		if !ok {
			return &MapError{Reason: SkipUnsupported, Detail: "enabled must be a boolean"}
		}
		if !enabled {
			return &MapError{Reason: SkipDisabled, Detail: "enabled is false; disabled servers are not imported"}
		}
	}
	if v, ok := raw["cwd"]; ok && v != nil {
		cwd, ok := v.(string)
		if !ok {
			return &MapError{Reason: SkipUnsupported, Detail: "cwd must be a string"}
		}
		if strings.TrimSpace(cwd) != "" {
			return &MapError{Reason: SkipUntransferredOption, Detail: "a working directory is set and was not transferred"}
		}
	}
	return nil
}

// openCodeLocalOptions applies execution constraints plus the native-array
// rules for timeout, oauth, and local headers. A tool-fetch timeout is
// warned and not copied.
func openCodeLocalOptions(raw map[string]any) ([]string, error) {
	if err := openCodeExecutionConstraints(raw); err != nil {
		return nil, err
	}
	var warnings []string
	if v, ok := raw["timeout"]; ok && v != nil {
		warnings = append(warnings, "timeout was not transferred; the OpenCode tool-fetch timeout is not represented in the stack")
	}
	if v, ok := raw["oauth"]; ok && v != nil {
		return nil, &MapError{Reason: SkipUntransferredOption, Detail: "oauth settings are not imported"}
	}
	if headers, ok := raw["headers"].(map[string]any); ok && len(headers) > 0 {
		return nil, &MapError{Reason: SkipUntransferredOption, Detail: "headers on a local entry are not imported"}
	}
	return warnings, nil
}

func openCodeEnvironment(raw map[string]any, translateLegacy bool) (map[string]string, error) {
	_, hasNative := raw["environment"]
	hasLegacy := mapHas(raw, "env") || mapHas(raw, "envs")
	if hasNative && hasLegacy {
		return nil, &MapError{Reason: SkipAmbiguous, Detail: "environment conflicts with env or envs; remove one map"}
	}
	if hasNative {
		return nativeEnvironment(raw["environment"])
	}
	env := envMap(raw)
	if translateLegacy {
		if err := translateEnvValues(env); err != nil {
			return nil, err
		}
	}
	return env, nil
}

func nativeEnvironment(v any) (map[string]string, error) {
	if v == nil {
		return nil, &MapError{Reason: SkipUnsupported, Detail: "environment is null"}
	}
	src, ok := v.(map[string]any)
	if !ok {
		return nil, &MapError{Reason: SkipUnsupported, Detail: "environment must be a map of strings"}
	}
	if len(src) == 0 {
		return nil, nil
	}
	env := make(map[string]string, len(src))
	for key, value := range src {
		if value == nil {
			return nil, &MapError{Reason: SkipUnsupported, Detail: "an environment value is null"}
		}
		s, ok := value.(string)
		if !ok {
			return nil, &MapError{Reason: SkipUnsupported, Detail: "environment values must be strings"}
		}
		next, err := translateNativeRef(s)
		if err != nil {
			return nil, err
		}
		env[key] = next
	}
	return env, nil
}

func translateEnvValues(env map[string]string) error {
	for key, value := range env {
		next, err := translateNativeRef(value)
		if err != nil {
			return err
		}
		env[key] = next
	}
	return nil
}

// translateNativeRef converts an exact whole-value {env:NAME} to ${NAME}
// when NAME is a shell identifier. It never reads the environment or a
// file. Invalid names, {file:...}, and embedded expressions skip the
// candidate instead of emitting a non-expanding literal. A leading $
// marks an already supported reference such as ${env:HOME}, which is
// left unchanged.
func translateNativeRef(v string) (string, error) {
	if !hasBareOpenCodeExpr(v) {
		return v, nil
	}
	if kind, name, ok := exactOpenCodeExpr(v); ok {
		switch kind {
		case "file":
			return "", &MapError{
				Reason: SkipNativeExpression,
				Detail: "native {file} expressions are not imported; the referenced file is not read. Replace the value with a literal or a ${NAME} reference",
			}
		case "env":
			if envNamePattern.MatchString(name) {
				return "${" + name + "}", nil
			}
			return "", &MapError{
				Reason: SkipNativeExpression,
				Detail: "native {env} name is not a shell identifier; only a whole-value {env:NAME} is converted to ${NAME}, and ${NAME} is expanded when the stack loads",
			}
		}
	}
	if hasBareOpenCodeKind(v, "{file:") {
		return "", &MapError{
			Reason: SkipNativeExpression,
			Detail: "native {file} expressions are not imported; the referenced file is not read",
		}
	}
	return "", &MapError{
		Reason: SkipNativeExpression,
		Detail: "embedded or unsupported native {env} expression; only a whole-value {env:NAME} with a shell identifier is converted to ${NAME}",
	}
}

func exactOpenCodeExpr(v string) (kind, name string, ok bool) {
	matches := nativeExprPattern.FindStringSubmatch(v)
	if len(matches) != 3 || matches[0] != v {
		return "", "", false
	}
	return matches[1], matches[2], true
}

func hasBareOpenCodeExpr(v string) bool {
	return hasBareOpenCodeKind(v, "{env:") || hasBareOpenCodeKind(v, "{file:")
}

func hasBareOpenCodeKind(v, kind string) bool {
	start := 0
	for {
		i := strings.Index(v[start:], kind)
		if i < 0 {
			return false
		}
		abs := start + i
		if abs == 0 || v[abs-1] != '$' {
			return true
		}
		start = abs + len(kind)
		if start > len(v) {
			return false
		}
	}
}

func mapHas(raw map[string]any, key string) bool {
	_, ok := raw[key]
	return ok
}
