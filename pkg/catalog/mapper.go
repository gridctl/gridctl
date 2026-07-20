package catalog

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gridctl/gridctl/pkg/config"
)

// Server assembles the config.MCPServer block for this entry. name is the
// stack server name; values maps input names to their final strings, either
// literals or ${var:KEY} references (the caller resolves prompting and
// vault routing first). Optional inputs with empty values are omitted.
func (e Entry) Server(name string, values map[string]string) (config.MCPServer, []string, error) {
	if e.Unsupported != "" {
		return config.MCPServer{}, nil, &UnsupportedInstallError{Kind: e.Unsupported}
	}

	var warnings []string
	env := make(map[string]string)
	var args []string
	var authValue string
	for _, in := range e.Inputs {
		v := values[in.Name]
		switch {
		case in.Auth:
			authValue = v
		case in.Arg:
			if v != "" {
				args = append(args, v)
			}
		default:
			if v != "" {
				env[in.Name] = v
			}
		}
	}
	if len(env) == 0 {
		env = nil
	}

	server := config.MCPServer{Name: name}
	switch e.Install.Type {
	case InstallImage:
		server.Image = e.Install.Image
		server.Port = e.Install.Port
		server.Transport = e.Install.Transport
		server.Env = env
		if len(args) > 0 {
			warnings = append(warnings, "positional inputs are not supported for container images and were dropped")
		}
	case InstallCommand:
		server.Command = append(append([]string(nil), e.Install.Command...), args...)
		server.Transport = "stdio"
		server.Env = env
	case InstallURL:
		server.URL = e.Install.URL
		server.Transport = e.Install.Transport
		switch e.Install.AuthType {
		case "bearer":
			server.Auth = &config.ServerAuth{Type: "bearer", Token: authValue}
		case "header":
			server.Auth = &config.ServerAuth{Type: "header", Header: e.Install.AuthHeader, Value: authValue}
		}
		if env != nil {
			warnings = append(warnings, "env inputs are not supported for external URL servers and were dropped")
		}
	default:
		return config.MCPServer{}, nil, &UnsupportedInstallError{Kind: fmt.Sprintf("install shape %q", e.Install.Type)}
	}
	return server, warnings, nil
}

// InstallLabel describes how the entry runs, for plans and tables:
// "stdio via npx", "container image", "http url".
func (e Entry) InstallLabel() string {
	switch e.Install.Type {
	case InstallImage:
		return "container image"
	case InstallCommand:
		runner := "command"
		if len(e.Install.Command) > 0 {
			runner = e.Install.Command[0]
		}
		return "stdio via " + runner
	case InstallURL:
		return e.Install.Transport + " url"
	default:
		return "unsupported"
	}
}

var serverNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// ServerName derives the stack server name for the entry: the part after
// the registry namespace slash, with characters outside [a-zA-Z0-9_-]
// collapsed to dashes.
func (e Entry) ServerName() string {
	name := e.Name
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return strings.Trim(serverNameSanitizer.ReplaceAllString(name, "-"), "-")
}

// fromRegistry converts one MCP Registry result into a catalog entry.
// Deleted entries convert like any other; callers filter them via Status
// (entriesFromResults already does). Entries with no supported install
// shape come back with Unsupported set rather than an error, so search can
// still show them.
func fromRegistry(r serverResult) Entry {
	e := Entry{
		Name:        r.Server.Name,
		Title:       r.Server.Title,
		Description: r.Server.Description,
		Tier:        TierRegistry,
		Homepage:    r.Server.WebsiteURL,
		Status:      StatusActive,
	}
	if r.Server.Repository != nil {
		e.Repository = r.Server.Repository.URL
	}
	if off := r.Meta.Official; off != nil && off.Status != "" {
		e.Status = off.Status
	}

	var unsupported []string
	for _, pick := range []string{"oci", "npm", "pypi"} {
		for _, pkg := range r.Server.Packages {
			if !strings.EqualFold(pkg.RegistryType, pick) {
				continue
			}
			install, inputs, why := installFromPackage(pkg)
			if why != "" {
				unsupported = append(unsupported, why)
				continue
			}
			e.Install = install
			e.Inputs = inputs
			return e
		}
	}
	for _, pkg := range r.Server.Packages {
		if !strings.EqualFold(pkg.RegistryType, "oci") &&
			!strings.EqualFold(pkg.RegistryType, "npm") &&
			!strings.EqualFold(pkg.RegistryType, "pypi") {
			unsupported = append(unsupported, strings.ToLower(pkg.RegistryType))
		}
	}

	if len(r.Server.Remotes) > 0 {
		install, inputs, why := installFromRemote(r.Server.Remotes[0])
		if why == "" {
			e.Install = install
			e.Inputs = inputs
			return e
		}
		unsupported = append(unsupported, why)
	}

	if len(unsupported) > 0 {
		e.Unsupported = strings.Join(unsupported, ", ")
	} else {
		e.Unsupported = "entry with no packages or remotes"
	}
	return e
}

// installFromPackage maps one registry package onto an install spec. The
// returned reason is non-empty when the package cannot be represented.
func installFromPackage(pkg registryPackage) (Install, []Input, string) {
	switch strings.ToLower(pkg.RegistryType) {
	case "oci":
		if t := normalizeTransport(pkg.Transport.Type); t != "stdio" {
			return Install{}, nil, fmt.Sprintf("oci (%s transport)", pkg.Transport.Type)
		}
		install := Install{Type: InstallImage, Transport: "stdio", Image: pkg.Identifier}
		inputs, warn := envInputs(pkg.EnvironmentVariables)
		if warn != "" {
			return Install{}, nil, warn
		}
		return install, inputs, ""
	case "npm":
		spec := pkg.Identifier
		if pkg.Version != "" {
			spec += "@" + pkg.Version
		}
		return commandInstall([]string{"npx", "-y", spec}, pkg)
	case "pypi":
		spec := pkg.Identifier
		if pkg.Version != "" {
			spec += "==" + pkg.Version
		}
		return commandInstall([]string{"uvx", spec}, pkg)
	default:
		return Install{}, nil, strings.ToLower(pkg.RegistryType)
	}
}

// commandInstall builds a local-process install from a base command plus
// the package's argument and env declarations. Input-backed positionals are
// appended by Entry.Server after the static command, so a static argument
// declared after one would be reordered on the final command line; those
// packages are rejected rather than silently corrupted.
func commandInstall(base []string, pkg registryPackage) (Install, []Input, string) {
	install := Install{Type: InstallCommand, Transport: "stdio", Command: base}
	inputs, warn := envInputs(pkg.EnvironmentVariables)
	if warn != "" {
		return Install{}, nil, warn
	}
	sawInputPositional := false
	for i, arg := range pkg.PackageArguments {
		static := arg.Value != "" && !strings.Contains(arg.Value, "{")
		switch {
		case arg.Type == "positional" && static:
			if sawInputPositional {
				return Install{}, nil, "static argument after a user-supplied positional"
			}
			install.Command = append(install.Command, arg.Value)
		case arg.Type == "positional" && arg.IsRequired:
			name := arg.ValueHint
			if name == "" {
				name = fmt.Sprintf("ARG_%d", i+1)
			}
			sawInputPositional = true
			inputs = append(inputs, Input{
				Name:        sanitizeInputName(name),
				Description: arg.Description,
				Required:    true,
				Secret:      arg.IsSecret,
				Arg:         true,
				Default:     arg.Default,
				Placeholder: arg.Placeholder,
				Choices:     arg.Choices,
				Format:      string(arg.Format),
			})
		case arg.Type == "named" && static:
			if sawInputPositional {
				return Install{}, nil, "static argument after a user-supplied positional"
			}
			install.Command = append(install.Command, arg.Name, arg.Value)
		case arg.IsRequired:
			return Install{}, nil, fmt.Sprintf("required argument %q with no static value", strings.TrimSpace(arg.Name+" "+arg.ValueHint))
		}
	}
	return install, inputs, ""
}

// envInputs converts registry environment-variable declarations. A warning
// reason is returned when a declaration needs templating we do not support.
func envInputs(vars []registryKeyValue) ([]Input, string) {
	var inputs []Input
	for _, v := range vars {
		if strings.Contains(v.Value, "{") {
			return nil, fmt.Sprintf("templated environment variable %q", v.Name)
		}
		def := v.Default
		if v.Value != "" {
			def = v.Value
		}
		inputs = append(inputs, Input{
			Name:        v.Name,
			Description: v.Description,
			Required:    v.IsRequired,
			Secret:      v.IsSecret,
			Default:     def,
			Placeholder: v.Placeholder,
			Choices:     v.Choices,
			Format:      string(v.Format),
		})
	}
	return inputs, ""
}

// installFromRemote maps a registry remote transport onto a url install.
func installFromRemote(remote registryTransport) (Install, []Input, string) {
	if strings.Contains(remote.URL, "{") {
		return Install{}, nil, "templated URL"
	}
	transport := normalizeTransport(remote.Type)
	if transport != "http" && transport != "sse" {
		return Install{}, nil, fmt.Sprintf("remote transport %q", remote.Type)
	}
	install := Install{Type: InstallURL, Transport: transport, URL: remote.URL}
	var inputs []Input
	for _, h := range remote.Headers {
		static := h.Value != "" && !strings.Contains(h.Value, "{")
		// Only auth-shaped headers claim the single auth slot config offers:
		// secret or required declarations, Authorization, or anything that
		// needs a user-supplied value. Informational static extras (e.g. a
		// fixed X-App-Version) have no representation and are dropped.
		authShaped := h.IsSecret || h.IsRequired || !static || strings.EqualFold(h.Name, "Authorization")
		if !authShaped {
			continue
		}
		if install.AuthType != "" {
			return Install{}, nil, "multiple authenticated headers"
		}
		if strings.EqualFold(h.Name, "Authorization") {
			install.AuthType = "bearer"
		} else {
			install.AuthType = "header"
			install.AuthHeader = h.Name
		}
		in := Input{
			Name:        sanitizeInputName(h.Name),
			Description: h.Description,
			Required:    h.IsRequired,
			Secret:      true, // auth material is always vault-routed
			Auth:        true,
			Placeholder: h.Placeholder,
		}
		if static {
			in.Default = h.Value
			in.Secret = h.IsSecret
		}
		inputs = append(inputs, in)
	}
	return install, inputs, ""
}

// normalizeTransport folds registry transport names into the stack.yaml
// vocabulary: streamable-http becomes http.
func normalizeTransport(t string) string {
	switch strings.ToLower(t) {
	case "streamable-http", "http", "streamable_http":
		return "http"
	case "sse":
		return "sse"
	case "stdio", "":
		return "stdio"
	default:
		return strings.ToLower(t)
	}
}

var inputNameSanitizer = regexp.MustCompile(`[^A-Z0-9_]+`)

// sanitizeInputName upper-snake-cases arbitrary hint names so they work as
// env keys and ${var:KEY} references (e.g. "Authorization" -> AUTHORIZATION,
// "file_path" -> FILE_PATH, "X-API-Key" -> X_API_KEY).
func sanitizeInputName(name string) string {
	upper := strings.ToUpper(name)
	return strings.Trim(inputNameSanitizer.ReplaceAllString(upper, "_"), "_")
}

var varKeySanitizer = regexp.MustCompile(`[^A-Za-z0-9_]+`)

// VarKey folds an input name into the ${var:KEY} reference grammar
// ([a-zA-Z_][a-zA-Z0-9_]*): invalid runs collapse to underscores and a
// leading digit is prefixed, so references built from registry-declared
// names (which may contain dashes) always resolve.
func VarKey(name string) string {
	key := strings.Trim(varKeySanitizer.ReplaceAllString(name, "_"), "_")
	if key == "" {
		return "VALUE"
	}
	if key[0] >= '0' && key[0] <= '9' {
		key = "_" + key
	}
	return key
}
