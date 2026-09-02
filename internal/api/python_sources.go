package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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
	StackName string                 `json:"stackName"`
	Server    pythonMCPServerRequest `json:"server"`
}

type pythonMCPServerRequest struct {
	Name               string                     `json:"name"`
	Image              string                     `json:"image,omitempty"`
	Source             *pythonSourceRequest       `json:"source,omitempty"`
	URL                string                     `json:"url,omitempty"`
	Port               int                        `json:"port,omitempty"`
	Transport          string                     `json:"transport,omitempty"`
	Command            []string                   `json:"command,omitempty"`
	Env                map[string]string          `json:"env,omitempty"`
	BuildArgs          map[string]string          `json:"buildArgs,omitempty"`
	Volumes            []string                   `json:"volumes,omitempty"`
	Network            string                     `json:"network,omitempty"`
	SSH                *config.SSHConfig          `json:"ssh,omitempty"`
	OpenAPI            *config.OpenAPIConfig      `json:"openapi,omitempty"`
	Tools              []string                   `json:"tools,omitempty"`
	OutputFormat       string                     `json:"outputFormat,omitempty"`
	PinSchemas         *bool                      `json:"pinSchemas,omitempty"`
	ReadyTimeout       string                     `json:"readyTimeout,omitempty"`
	PingTimeout        string                     `json:"pingTimeout,omitempty"`
	ProtocolGeneration string                     `json:"protocolGeneration,omitempty"`
	Replicas           int                        `json:"replicas,omitempty"`
	ReplicaPolicy      string                     `json:"replicaPolicy,omitempty"`
	Autoscale          *pythonAutoscaleRequest    `json:"autoscale,omitempty"`
	Telemetry          *config.MCPServerTelemetry `json:"telemetry,omitempty"`
	Auth               *config.ServerAuth         `json:"auth,omitempty"`
}

type pythonSourceRequest struct {
	Type        string                   `json:"type"`
	URL         string                   `json:"url,omitempty"`
	Ref         string                   `json:"ref,omitempty"`
	Path        string                   `json:"path,omitempty"`
	ProjectPath string                   `json:"projectPath,omitempty"`
	Dockerfile  string                   `json:"dockerfile,omitempty"`
	Runtime     string                   `json:"runtime,omitempty"`
	Package     string                   `json:"package,omitempty"`
	Python      string                   `json:"python,omitempty"`
	Extras      []string                 `json:"extras,omitempty"`
	With        []string                 `json:"with,omitempty"`
	Packages    []string                 `json:"packages,omitempty"`
	Auth        *pythonSourceAuthRequest `json:"auth,omitempty"`
}

type pythonSourceAuthRequest struct {
	Method        string `json:"method,omitempty"`
	CredentialRef string `json:"credentialRef,omitempty"`
	SSHUser       string `json:"sshUser,omitempty"`
	SSHKeyPath    string `json:"sshKeyPath,omitempty"`
}

type pythonAutoscaleRequest struct {
	Min            int    `json:"min"`
	Max            int    `json:"max"`
	TargetInFlight int    `json:"targetInFlight"`
	ScaleUpAfter   string `json:"scaleUpAfter,omitempty"`
	ScaleDownAfter string `json:"scaleDownAfter,omitempty"`
	WarmPool       int    `json:"warmPool,omitempty"`
	IdleToZero     bool   `json:"idleToZero,omitempty"`
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
	result := config.ValidateWithIssues(&stack)
	trimValidationIssuePrefix(result, req.ResourceType)
	writeJSON(w, result)
}

func trimValidationIssuePrefix(result *config.ValidationResult, resourceType string) {
	prefix := "resources[0]"
	if resourceType == "mcp-server" {
		prefix = "mcp-servers[0]"
	}
	for i := range result.Issues {
		result.Issues[i].Field = strings.TrimPrefix(result.Issues[i].Field, prefix+".")
		if result.Issues[i].Field == prefix {
			result.Issues[i].Field = ""
		}
	}
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
	stack := config.Stack{Name: req.StackName, Network: config.Network{Name: "preview"}, MCPServers: []config.MCPServer{req.Server.config()}}
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
		auth, err := runtime.AuthForSource(server.Source.Auth, server.Source.URL, runtime.CredentialResolver(s.credentialResolver()))
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

func (r pythonMCPServerRequest) config() config.MCPServer {
	server := config.MCPServer{
		Name: r.Name, Image: r.Image, URL: r.URL, Port: r.Port, Transport: r.Transport,
		Command: r.Command, Env: r.Env, BuildArgs: r.BuildArgs, Volumes: r.Volumes,
		Network: r.Network, SSH: r.SSH, OpenAPI: r.OpenAPI, Tools: r.Tools,
		OutputFormat: r.OutputFormat, PinSchemas: r.PinSchemas, ReadyTimeout: r.ReadyTimeout,
		PingTimeout: r.PingTimeout, ProtocolGeneration: r.ProtocolGeneration,
		Replicas: r.Replicas, ReplicaPolicy: r.ReplicaPolicy, Telemetry: r.Telemetry, Auth: r.Auth,
	}
	if r.Source != nil {
		server.Source = r.Source.config()
	}
	if r.Autoscale != nil {
		server.Autoscale = &config.AutoscaleConfig{
			Min: r.Autoscale.Min, Max: r.Autoscale.Max, TargetInFlight: r.Autoscale.TargetInFlight,
			ScaleUpAfter: r.Autoscale.ScaleUpAfter, ScaleDownAfter: r.Autoscale.ScaleDownAfter,
			WarmPool: r.Autoscale.WarmPool, IdleToZero: r.Autoscale.IdleToZero,
		}
	}
	return server
}

func (r pythonSourceRequest) config() *config.Source {
	source := &config.Source{
		Type: r.Type, URL: r.URL, Ref: r.Ref, Path: r.Path, ProjectPath: r.ProjectPath,
		Dockerfile: r.Dockerfile, Runtime: r.Runtime, Package: r.Package, Python: r.Python,
		Extras: r.Extras, With: r.With, Packages: r.Packages,
	}
	if r.Auth != nil {
		source.Auth = &config.SourceAuth{
			Method: r.Auth.Method, CredentialRef: r.Auth.CredentialRef,
			SSHUser: r.Auth.SSHUser, SSHKeyPath: r.Auth.SSHKeyPath,
		}
	}
	return source
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
		slog.Warn("status: failed to read stack for MCP server provenance", "path", s.stackFile, "error", err)
		return
	}
	var stack config.Stack
	if err := yaml.Unmarshal(data, &stack); err != nil {
		slog.Warn("status: failed to parse stack for MCP server provenance", "path", s.stackFile, "error", err)
		return
	}
	declared := make(map[string]config.MCPServer, len(stack.MCPServers))
	for _, server := range stack.MCPServers {
		declared[server.Name] = server
	}
	images := make(map[string]string)
	if s.dockerClient != nil && s.stackName != "" {
		containers, listErr := runtimedocker.ListManagedContainers(ctx, s.dockerClient, s.stackName)
		if listErr != nil {
			slog.Warn("status: failed to list MCP server containers; omitting image provenance", "stack", s.stackName, "error", listErr)
		} else {
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
		slog.Warn("status: failed to inspect MCP server image provenance", "error", err)
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
