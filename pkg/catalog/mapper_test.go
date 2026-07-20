package catalog

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestEntryServer_Shapes(t *testing.T) {
	tests := []struct {
		name    string
		entry   Entry
		values  map[string]string
		want    func(t *testing.T, got serverShape)
		wantErr string
	}{
		{
			name: "image install",
			entry: Entry{
				Install: Install{Type: InstallImage, Transport: "stdio", Image: "ghcr.io/github/github-mcp-server"},
				Inputs:  []Input{{Name: "GITHUB_PERSONAL_ACCESS_TOKEN", Required: true, Secret: true}},
			},
			values: map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": "${var:GITHUB_PERSONAL_ACCESS_TOKEN}"},
			want: func(t *testing.T, got serverShape) {
				if got.Image != "ghcr.io/github/github-mcp-server" || got.Transport != "stdio" {
					t.Errorf("image/transport = %q/%q", got.Image, got.Transport)
				}
				if got.Env["GITHUB_PERSONAL_ACCESS_TOKEN"] != "${var:GITHUB_PERSONAL_ACCESS_TOKEN}" {
					t.Errorf("env = %v", got.Env)
				}
			},
		},
		{
			name: "command install with positional arg",
			entry: Entry{
				Install: Install{Type: InstallCommand, Transport: "stdio", Command: []string{"npx", "-y", "@modelcontextprotocol/server-filesystem"}},
				Inputs:  []Input{{Name: "ALLOWED_DIR", Required: true, Arg: true}},
			},
			values: map[string]string{"ALLOWED_DIR": "/tmp/data"},
			want: func(t *testing.T, got serverShape) {
				wantCmd := []string{"npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp/data"}
				if !reflect.DeepEqual(got.Command, wantCmd) {
					t.Errorf("command = %v, want %v", got.Command, wantCmd)
				}
				if len(got.Env) != 0 {
					t.Errorf("arg input leaked into env: %v", got.Env)
				}
			},
		},
		{
			name: "url install with bearer auth",
			entry: Entry{
				Install: Install{Type: InstallURL, Transport: "http", URL: "https://example.com/mcp", AuthType: "bearer"},
				Inputs:  []Input{{Name: "TOKEN", Auth: true, Secret: true}},
			},
			values: map[string]string{"TOKEN": "${var:TOKEN}"},
			want: func(t *testing.T, got serverShape) {
				if got.URL != "https://example.com/mcp" {
					t.Errorf("url = %q", got.URL)
				}
				if got.AuthType != "bearer" || got.AuthToken != "${var:TOKEN}" {
					t.Errorf("auth = %q/%q", got.AuthType, got.AuthToken)
				}
			},
		},
		{
			name: "url install with header auth",
			entry: Entry{
				Install: Install{Type: InstallURL, Transport: "sse", URL: "https://example.com/sse", AuthType: "header", AuthHeader: "X-API-Key"},
				Inputs:  []Input{{Name: "X_API_KEY", Auth: true, Secret: true}},
			},
			values: map[string]string{"X_API_KEY": "${var:X_API_KEY}"},
			want: func(t *testing.T, got serverShape) {
				if got.AuthType != "header" || got.AuthHeader != "X-API-Key" || got.AuthValue != "${var:X_API_KEY}" {
					t.Errorf("auth = %q/%q/%q", got.AuthType, got.AuthHeader, got.AuthValue)
				}
			},
		},
		{
			name:    "unsupported entry",
			entry:   Entry{Unsupported: "mcpb"},
			wantErr: "unsupported package type mcpb",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _, err := tt.entry.Server("test", tt.values)
			if tt.wantErr != "" {
				var unsupported *UnsupportedInstallError
				if !errors.As(err, &unsupported) {
					t.Fatalf("error = %v, want UnsupportedInstallError", err)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			shape := serverShape{
				Image: server.Image, Transport: server.Transport, URL: server.URL,
				Command: server.Command, Env: server.Env,
			}
			if server.Auth != nil {
				shape.AuthType = server.Auth.Type
				shape.AuthToken = server.Auth.Token
				shape.AuthHeader = server.Auth.Header
				shape.AuthValue = server.Auth.Value
			}
			tt.want(t, shape)
		})
	}
}

// serverShape flattens the interesting fields for assertions.
type serverShape struct {
	Image, Transport, URL string
	Command               []string
	Env                   map[string]string
	AuthType, AuthToken   string
	AuthHeader, AuthValue string
}

func TestEntryServer_OmitsOptionalUnsetInputs(t *testing.T) {
	entry := Entry{
		Install: Install{Type: InstallCommand, Transport: "stdio", Command: []string{"npx", "-y", "pkg"}},
		Inputs: []Input{
			{Name: "REQUIRED_KEY", Required: true},
			{Name: "OPTIONAL_KEY"},
		},
	}
	server, _, err := entry.Server("test", map[string]string{"REQUIRED_KEY": "${var:REQUIRED_KEY}"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := server.Env["OPTIONAL_KEY"]; ok {
		t.Error("unset optional input should be omitted from env")
	}
	if server.Env["REQUIRED_KEY"] != "${var:REQUIRED_KEY}" {
		t.Errorf("env = %v", server.Env)
	}
}

func TestFromRegistry_PackageShapes(t *testing.T) {
	tests := []struct {
		name        string
		result      serverResult
		wantType    string
		wantCommand []string
		wantImage   string
		wantURL     string
		unsupported bool
	}{
		{
			name: "oci stdio package",
			result: registryResult("io.github.acme/files", []registryPackage{{
				RegistryType: "oci", Identifier: "ghcr.io/acme/files:1.0.0",
				Transport: registryTransport{Type: "stdio"},
			}}, nil),
			wantType:  InstallImage,
			wantImage: "ghcr.io/acme/files:1.0.0",
		},
		{
			name: "npm package pins exact version",
			result: registryResult("io.github.acme/npm-server", []registryPackage{{
				RegistryType: "npm", Identifier: "@acme/server", Version: "1.2.3",
				Transport: registryTransport{Type: "stdio"},
			}}, nil),
			wantType:    InstallCommand,
			wantCommand: []string{"npx", "-y", "@acme/server@1.2.3"},
		},
		{
			name: "pypi package pins exact version",
			result: registryResult("io.github.acme/py-server", []registryPackage{{
				RegistryType: "pypi", Identifier: "acme-server", Version: "0.5.0",
				Transport: registryTransport{Type: "stdio"},
			}}, nil),
			wantType:    InstallCommand,
			wantCommand: []string{"uvx", "acme-server==0.5.0"},
		},
		{
			name: "oci preferred over npm",
			result: registryResult("io.github.acme/both", []registryPackage{
				{RegistryType: "npm", Identifier: "@acme/server", Version: "1.0.0", Transport: registryTransport{Type: "stdio"}},
				{RegistryType: "oci", Identifier: "ghcr.io/acme/server:1", Transport: registryTransport{Type: "stdio"}},
			}, nil),
			wantType:  InstallImage,
			wantImage: "ghcr.io/acme/server:1",
		},
		{
			name: "remote streamable-http normalizes to http",
			result: registryResult("io.github.acme/remote", nil,
				[]registryTransport{{Type: "streamable-http", URL: "https://acme.com/mcp"}}),
			wantType: InstallURL,
			wantURL:  "https://acme.com/mcp",
		},
		{
			name: "mcpb is unsupported",
			result: registryResult("io.github.acme/bundle", []registryPackage{{
				RegistryType: "mcpb", Identifier: "https://example.com/b.mcpb",
				Transport: registryTransport{Type: "stdio"},
			}}, nil),
			unsupported: true,
		},
		{
			name: "nuget and cargo are unsupported",
			result: registryResult("io.github.acme/dotnet", []registryPackage{
				{RegistryType: "nuget", Identifier: "Acme.Server", Version: "1.0.0", Transport: registryTransport{Type: "stdio"}},
				{RegistryType: "cargo", Identifier: "acme-server", Version: "1.0.0", Transport: registryTransport{Type: "stdio"}},
			}, nil),
			unsupported: true,
		},
		{
			name: "templated remote URL is unsupported",
			result: registryResult("io.github.acme/tenant", nil,
				[]registryTransport{{Type: "streamable-http", URL: "https://{tenant}.acme.com/mcp"}}),
			unsupported: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := fromRegistry(tt.result)
			if entry.Tier != TierRegistry {
				t.Errorf("tier = %q", entry.Tier)
			}
			if tt.unsupported {
				if entry.Unsupported == "" {
					t.Fatalf("expected unsupported, got install %+v", entry.Install)
				}
				if _, _, err := entry.Server("x", nil); err == nil {
					t.Fatal("Server should reject unsupported entries")
				}
				return
			}
			if entry.Unsupported != "" {
				t.Fatalf("unexpected unsupported: %q", entry.Unsupported)
			}
			if entry.Install.Type != tt.wantType {
				t.Errorf("install type = %q, want %q", entry.Install.Type, tt.wantType)
			}
			if tt.wantImage != "" && entry.Install.Image != tt.wantImage {
				t.Errorf("image = %q, want %q", entry.Install.Image, tt.wantImage)
			}
			if tt.wantCommand != nil && !reflect.DeepEqual(entry.Install.Command, tt.wantCommand) {
				t.Errorf("command = %v, want %v", entry.Install.Command, tt.wantCommand)
			}
			if tt.wantURL != "" && entry.Install.URL != tt.wantURL {
				t.Errorf("url = %q, want %q", entry.Install.URL, tt.wantURL)
			}
		})
	}
}

func TestFromRegistry_EnvAndArgsAndHeaders(t *testing.T) {
	t.Run("env vars become inputs", func(t *testing.T) {
		result := registryResult("io.github.acme/envy", []registryPackage{{
			RegistryType: "npm", Identifier: "@acme/envy", Version: "1.0.0",
			Transport: registryTransport{Type: "stdio"},
			EnvironmentVariables: []registryKeyValue{
				{Name: "API_KEY", registryInput: registryInput{IsRequired: true, IsSecret: true}},
				{Name: "REGION", registryInput: registryInput{Default: "us-east-1"}},
			},
		}}, nil)
		entry := fromRegistry(result)
		if len(entry.Inputs) != 2 {
			t.Fatalf("inputs = %+v", entry.Inputs)
		}
		if !entry.Inputs[0].Required || !entry.Inputs[0].Secret {
			t.Errorf("API_KEY flags lost: %+v", entry.Inputs[0])
		}
		if entry.Inputs[1].Default != "us-east-1" {
			t.Errorf("REGION default lost: %+v", entry.Inputs[1])
		}
	})

	t.Run("required positional argument becomes an arg input", func(t *testing.T) {
		result := registryResult("io.github.acme/fs", []registryPackage{{
			RegistryType: "npm", Identifier: "@acme/fs", Version: "1.0.0",
			Transport: registryTransport{Type: "stdio"},
			PackageArguments: []registryArgument{
				{Type: "positional", ValueHint: "root_path", registryInput: registryInput{IsRequired: true, Format: "filepath"}},
			},
		}}, nil)
		entry := fromRegistry(result)
		if len(entry.Inputs) != 1 || !entry.Inputs[0].Arg || entry.Inputs[0].Name != "ROOT_PATH" {
			t.Fatalf("inputs = %+v", entry.Inputs)
		}
	})

	t.Run("authorization header maps to bearer auth", func(t *testing.T) {
		result := registryResult("io.github.acme/api", nil, []registryTransport{{
			Type: "sse", URL: "https://acme.com/sse",
			Headers: []registryKeyValue{{Name: "Authorization", registryInput: registryInput{IsRequired: true, IsSecret: true}}},
		}})
		entry := fromRegistry(result)
		if entry.Install.AuthType != "bearer" {
			t.Fatalf("auth type = %q, install %+v", entry.Install.AuthType, entry.Install)
		}
		if len(entry.Inputs) != 1 || !entry.Inputs[0].Auth || !entry.Inputs[0].Secret {
			t.Fatalf("inputs = %+v", entry.Inputs)
		}
	})
}

func TestFromRegistry_Status(t *testing.T) {
	deprecated := registryResult("io.github.acme/old", nil,
		[]registryTransport{{Type: "sse", URL: "https://acme.com/sse"}})
	deprecated.Meta.Official = &registryOfficial{Status: "deprecated"}
	if got := fromRegistry(deprecated).Status; got != StatusDeprecated {
		t.Errorf("status = %q, want deprecated", got)
	}

	deleted := registryResult("io.github.acme/gone", nil,
		[]registryTransport{{Type: "sse", URL: "https://acme.com/sse"}})
	deleted.Meta.Official = &registryOfficial{Status: "deleted"}
	if entries := entriesFromResults([]serverResult{deleted}); len(entries) != 0 {
		t.Errorf("deleted entries must be filtered, got %d", len(entries))
	}
}

func TestServerName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"github", "github"},
		{"io.github.user/weather", "weather"},
		{"io.github.user/My Server!", "My-Server"},
	}
	for _, tt := range tests {
		if got := (Entry{Name: tt.in}).ServerName(); got != tt.want {
			t.Errorf("ServerName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// registryResult builds a minimal active serverResult.
func registryResult(name string, packages []registryPackage, remotes []registryTransport) serverResult {
	var r serverResult
	r.Server.Name = name
	r.Server.Description = "test server"
	r.Server.Packages = packages
	r.Server.Remotes = remotes
	return r
}

func TestCommandInstall_RejectsStaticAfterInputPositional(t *testing.T) {
	// A static positional declared after a user-supplied one would be
	// reordered on the final command line; the package must be rejected.
	result := registryResult("io.github.acme/fs", []registryPackage{{
		RegistryType: "npm", Identifier: "@acme/fs", Version: "1.0.0",
		Transport: registryTransport{Type: "stdio"},
		PackageArguments: []registryArgument{
			{Type: "positional", ValueHint: "root_dir", registryInput: registryInput{IsRequired: true}},
			{Type: "positional", registryInput: registryInput{Value: "readonly"}},
		},
	}}, nil)
	entry := fromRegistry(result)
	if entry.Unsupported == "" {
		t.Fatalf("expected unsupported, got install %+v", entry.Install)
	}

	// The safe order (static first, input second) stays supported.
	result.Server.Packages[0].PackageArguments = []registryArgument{
		{Type: "positional", registryInput: registryInput{Value: "readonly"}},
		{Type: "positional", ValueHint: "root_dir", registryInput: registryInput{IsRequired: true}},
	}
	entry = fromRegistry(result)
	if entry.Unsupported != "" {
		t.Fatalf("static-before-input should be supported: %q", entry.Unsupported)
	}
	server, _, err := entry.Server("fs", map[string]string{"ROOT_DIR": "/data"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"npx", "-y", "@acme/fs@1.0.0", "readonly", "/data"}
	if !reflect.DeepEqual(server.Command, want) {
		t.Errorf("command = %v, want %v", server.Command, want)
	}
}

func TestInstallFromRemote_StaticExtraHeaderDoesNotClaimAuth(t *testing.T) {
	// An informational static header must not block a genuine auth header.
	result := registryResult("io.github.acme/api", nil, []registryTransport{{
		Type: "streamable-http", URL: "https://acme.com/mcp",
		Headers: []registryKeyValue{
			{Name: "X-App-Version", registryInput: registryInput{Value: "1.0"}},
			{Name: "Authorization", registryInput: registryInput{IsRequired: true, IsSecret: true}},
		},
	}})
	entry := fromRegistry(result)
	if entry.Unsupported != "" {
		t.Fatalf("unexpected unsupported: %q", entry.Unsupported)
	}
	if entry.Install.AuthType != "bearer" {
		t.Errorf("auth type = %q, want bearer", entry.Install.AuthType)
	}
	if len(entry.Inputs) != 1 || entry.Inputs[0].Name != "AUTHORIZATION" {
		t.Errorf("inputs = %+v", entry.Inputs)
	}
}

func TestInstallLabel(t *testing.T) {
	tests := []struct {
		entry Entry
		want  string
	}{
		{Entry{Install: Install{Type: InstallImage}}, "container image"},
		{Entry{Install: Install{Type: InstallCommand, Command: []string{"npx", "-y", "pkg"}}}, "stdio via npx"},
		{Entry{Install: Install{Type: InstallURL, Transport: "http"}}, "http url"},
		{Entry{Unsupported: "mcpb"}, "unsupported"},
	}
	for _, tt := range tests {
		if got := tt.entry.InstallLabel(); got != tt.want {
			t.Errorf("InstallLabel(%+v) = %q, want %q", tt.entry.Install, got, tt.want)
		}
	}
}

func TestVarKey(t *testing.T) {
	tests := []struct{ in, want string }{
		{"GITHUB_TOKEN", "GITHUB_TOKEN"},
		{"api-key", "api_key"},
		{"123abc", "_123abc"},
		{"--", "VALUE"},
	}
	for _, tt := range tests {
		if got := VarKey(tt.in); got != tt.want {
			t.Errorf("VarKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
