package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/gridctl/gridctl/pkg/builder"
	"github.com/gridctl/gridctl/pkg/config"
	gitpkg "github.com/gridctl/gridctl/pkg/git"
	"github.com/gridctl/gridctl/pkg/runtime"
	runtimedocker "github.com/gridctl/gridctl/pkg/runtime/docker"
	"gopkg.in/yaml.v3"
)

const maxPythonAPIRequest = 1 << 20

type pythonSourcePlanner interface {
	Plan(ctx context.Context, opts builder.BuildOptions) (*builder.ResolvedBuildPlan, error)
	Versions(ctx context.Context, project string) (*builder.PyPIVersions, error)
}

type pythonResolveRequest struct {
	StackName string           `json:"stackName"`
	Server    config.MCPServer `json:"server"`
}

type generatedFileResponse struct {
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Content   string `json:"content"`
}

type pythonResolutionResponse struct {
	DeclaredIdentity    builder.SourceIdentity  `json:"declaredIdentity"`
	ResolvedIdentity    builder.SourceIdentity  `json:"resolvedIdentity"`
	Python              string                  `json:"python,omitempty"`
	Command             []string                `json:"command,omitempty"`
	BuildInputDigest    string                  `json:"buildInputDigest"`
	ImageTag            string                  `json:"imageTag"`
	Cached              bool                    `json:"cached"`
	MutableRef          bool                    `json:"mutableRef"`
	Provenance          builder.BuildProvenance `json:"provenance"`
	GeneratedDockerfile *generatedFileResponse  `json:"generatedFile,omitempty"`
}

func (s *Server) handleStackResourceValidate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ResourceType string `json:"resourceType"`
		YAML         string `json:"yaml"`
	}
	if err := decodeBoundedJSON(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	stack := config.Stack{Name: "wizard-preview", Network: config.Network{Name: "wizard-preview"}}
	switch req.ResourceType {
	case "mcp-server":
		var server config.MCPServer
		if err := yaml.Unmarshal([]byte(req.YAML), &server); err != nil {
			writeResourceYAMLError(w, err)
			return
		}
		stack.MCPServers = []config.MCPServer{server}
	case "resource":
		var resource config.Resource
		if err := yaml.Unmarshal([]byte(req.YAML), &resource); err != nil {
			writeResourceYAMLError(w, err)
			return
		}
		stack.Resources = []config.Resource{resource}
	default:
		writeJSONError(w, "Unsupported resourceType: "+req.ResourceType, http.StatusBadRequest)
		return
	}
	if err := config.ExpandStackVarsWithEnvChecked(&stack); err != nil {
		writeJSON(w, validationError("variables", err.Error()))
		return
	}
	stack.SetDefaults()
	writeJSON(w, config.ValidateWithIssues(&stack))
}

func writeResourceYAMLError(w http.ResponseWriter, err error) {
	writeJSON(w, validationError("yaml", "YAML parse error: "+err.Error()))
}

func validationError(field, message string) *config.ValidationResult {
	return &config.ValidationResult{Valid: false, ErrorCount: 1, Issues: []config.ValidationIssue{{
		Field: field, Message: message, Severity: config.SeverityError,
	}}}
}

func (s *Server) handlePythonPackageVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := s.pythonSourceService().Versions(r.Context(), r.PathValue("package"))
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	writeJSON(w, versions)
}

func (s *Server) handlePythonSourceResolve(w http.ResponseWriter, r *http.Request) {
	resolution, err := s.resolvePythonSource(r)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	writeJSON(w, resolution)
}

func (s *Server) handlePythonGeneratedFile(w http.ResponseWriter, r *http.Request) {
	resolution, err := s.resolvePythonSource(r)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if resolution.GeneratedDockerfile == nil {
		writeJSONError(w, "server does not use a generated Dockerfile", http.StatusUnprocessableEntity)
		return
	}
	writeJSON(w, resolution.GeneratedDockerfile)
}

func (s *Server) resolvePythonSource(r *http.Request) (*pythonResolutionResponse, error) {
	var req pythonResolveRequest
	if err := decodeBoundedJSON(r, &req); err != nil {
		return nil, err
	}
	if req.StackName == "" {
		req.StackName = "preview"
	}
	stack := config.Stack{Name: req.StackName, Network: config.Network{Name: "preview"}, MCPServers: []config.MCPServer{req.Server}}
	stack.SetDefaults()
	if result := config.ValidateWithIssues(&stack); !result.Valid {
		return nil, fmt.Errorf("invalid MCP server: %s", formatValidationIssues(result.Issues))
	}
	server := &stack.MCPServers[0]
	if server.Source == nil || (server.Source.Type != "pypi" && server.Source.Runtime != "python") {
		return nil, fmt.Errorf("server must declare a PyPI source or source.runtime python")
	}
	opts := pythonBuildOptions(req.StackName, server)
	if server.Source.Auth != nil {
		auth, err := runtime.AuthForSource(server.Source.Auth, server.Source.URL, s.sourceCredentialResolver)
		if err != nil {
			return nil, fmt.Errorf("resolving source auth: %w", err)
		}
		opts.Auth = auth
	}
	plan, err := s.pythonSourceService().Plan(r.Context(), opts)
	if err != nil {
		return nil, redactPythonSourceError(err, server.Source.URL)
	}
	defer func() { _ = plan.Close() }()

	response := &pythonResolutionResponse{
		DeclaredIdentity: redactSourceIdentity(plan.DeclaredIdentity),
		ResolvedIdentity: redactSourceIdentity(plan.ResolvedIdentity),
		Python:           plan.Python,
		Command:          plan.Command,
		BuildInputDigest: plan.BuildInputDigest,
		ImageTag:         plan.ImageTag,
		Cached:           plan.Cached,
		MutableRef:       plan.MutableRef,
		Provenance:       plan.Provenance,
	}
	if plan.GeneratedDockerfile != "" {
		response.GeneratedDockerfile = &generatedFileResponse{Name: ".gridctl.Dockerfile", MediaType: "text/x-dockerfile", Content: plan.GeneratedDockerfile}
	}
	return response, nil
}

func (s *Server) pythonSourceService() pythonSourcePlanner {
	if s.pythonSources == nil {
		s.pythonSources = builder.New(s.dockerClient)
	}
	return s.pythonSources
}

func pythonBuildOptions(stackName string, server *config.MCPServer) builder.BuildOptions {
	source := server.Source
	return builder.BuildOptions{
		Stack: stackName, ServerName: server.Name, SourceType: source.Type,
		URL: source.URL, Ref: source.Ref, Path: source.Path, ProjectPath: source.ProjectPath,
		Runtime: source.Runtime, Package: source.Package, Python: source.Python,
		Extras: source.Extras, With: source.With, Packages: source.Packages,
		Dockerfile: source.Dockerfile, BuildArgs: server.BuildArgs, Command: server.Command,
	}
}

func (s *Server) sourceCredentialResolver(ref string) (string, error) {
	if s.vaultStore == nil {
		return "", fmt.Errorf("variable store is not configured")
	}
	key := strings.TrimSuffix(strings.TrimPrefix(ref, "${var:"), "}")
	if key == ref {
		key = strings.TrimSuffix(strings.TrimPrefix(ref, "${vault:"), "}")
	}
	if key == ref || key == "" {
		return "", fmt.Errorf("credential reference must use ${var:KEY}")
	}
	value, ok := s.vaultStore.Get(key)
	if !ok {
		return "", fmt.Errorf("variable %q was not found", key)
	}
	return value, nil
}

func decodeBoundedJSON(r *http.Request, dst any) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxPythonAPIRequest+1))
	if err != nil {
		return fmt.Errorf("failed to read request body: %w", err)
	}
	if len(data) > maxPythonAPIRequest {
		return fmt.Errorf("request body exceeds %d bytes", maxPythonAPIRequest)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("invalid JSON: request must contain one object")
	}
	return nil
}

func formatValidationIssues(issues []config.ValidationIssue) string {
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		if issue.Severity == config.SeverityError {
			parts = append(parts, issue.Field+": "+issue.Message)
		}
	}
	return strings.Join(parts, "; ")
}

func redactSourceIdentity(identity builder.SourceIdentity) builder.SourceIdentity {
	identity.URL = gitpkg.RedactURL(identity.URL)
	return identity
}

func redactPythonSourceError(err error, sourceURL string) error {
	message := err.Error()
	if sourceURL != "" {
		message = strings.ReplaceAll(message, sourceURL, gitpkg.RedactURL(sourceURL))
	}
	return fmt.Errorf("%s", message)
}

func (s *Server) enrichMCPServerStatuses(ctx context.Context, statuses []MCPServerStatus) {
	if len(statuses) == 0 || s.stackFile == "" {
		return
	}
	data, err := os.ReadFile(s.stackFile)
	if err != nil {
		return
	}
	var stack config.Stack
	if err := yaml.Unmarshal(data, &stack); err != nil {
		return
	}
	declared := make(map[string]config.MCPServer, len(stack.MCPServers))
	for _, server := range stack.MCPServers {
		declared[server.Name] = server
	}
	images := make(map[string]string)
	if s.dockerClient != nil && s.stackName != "" {
		containers, listErr := runtimedocker.ListManagedContainers(ctx, s.dockerClient, s.stackName)
		if listErr == nil {
			for _, container := range containers {
				if name := container.Labels[runtimedocker.LabelMCPServer]; name != "" && images[name] == "" {
					images[name] = container.Image
				}
			}
		}
	}
	for i := range statuses {
		server, ok := declared[statuses[i].Name]
		if !ok {
			continue
		}
		statuses[i].Image = images[server.Name]
		if server.Source == nil {
			if server.Image != "" {
				statuses[i].Kind = "Container"
				if statuses[i].Image == "" {
					statuses[i].Image = server.Image
				}
			}
			continue
		}
		statuses[i].Kind = "Source container"
		if (server.Source.Type == "pypi" || server.Source.Runtime == "python") && server.Source.Dockerfile == "" {
			statuses[i].Kind = "Python container"
		}
		statuses[i].Source = &MCPServerSourceStatus{
			Type: server.Source.Type, URL: gitpkg.RedactURL(server.Source.URL), Ref: server.Source.Ref,
			Package: server.Source.Package,
		}
		if server.Source.Type == "pypi" {
			statuses[i].Source.Version = server.Source.Ref
		}
		if statuses[i].Image != "" {
			s.enrichSourceFromImage(ctx, statuses[i].Image, statuses[i].Source)
		}
	}
}

func (s *Server) enrichSourceFromImage(ctx context.Context, imageTag string, source *MCPServerSourceStatus) {
	images, err := s.dockerClient.ImageList(ctx, image.ListOptions{Filters: filters.NewArgs(filters.Arg("reference", imageTag))})
	if err != nil {
		return
	}
	for _, candidate := range images {
		if candidate.Labels == nil {
			continue
		}
		source.Commit = candidate.Labels["org.opencontainers.image.revision"]
		if value := candidate.Labels[builder.LabelSourcePackage]; value != "" {
			source.Package = value
		}
		if value := candidate.Labels[builder.LabelSourceVersion]; value != "" {
			source.Version = value
		}
		source.Artifact = candidate.Labels[builder.LabelSourceArtifact]
		return
	}
}
