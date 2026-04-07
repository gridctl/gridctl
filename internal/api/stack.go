package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/state"

	"gopkg.in/yaml.v3"
)

// handleStackValidate validates a stack YAML body.
// POST /api/stack/validate
func (s *Server) handleStackValidate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		writeJSONError(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var stack config.Stack
	if err := yaml.Unmarshal(body, &stack); err != nil {
		writeJSON(w, &config.ValidationResult{
			Valid:      false,
			ErrorCount: 1,
			Issues: []config.ValidationIssue{{
				Field:    "yaml",
				Message:  "YAML parse error: " + err.Error(),
				Severity: config.SeverityError,
			}},
		})
		return
	}

	// Match CLI validation: expand env vars, apply defaults
	config.ExpandStackVarsWithEnv(&stack)
	stack.SetDefaults()
	result := config.ValidateWithIssues(&stack)
	writeJSON(w, result)
}

// handleStackPlan compares current spec against running state.
// GET /api/stack/plan
func (s *Server) handleStackPlan(w http.ResponseWriter, r *http.Request) {
	if s.stackFile == "" || s.stackName == "" {
		writeJSONError(w, "No stack is currently deployed", http.StatusServiceUnavailable)
		return
	}

	// Load the current spec from the stack file
	proposed, _, err := config.ValidateStackFile(s.stackFile)
	if err != nil {
		writeJSONError(w, "Failed to load stack file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Load the running state's spec
	current := s.loadRunningSpec()

	diff := config.ComputePlan(proposed, current)
	writeJSON(w, diff)
}

// handleStackHealth returns aggregate spec health.
// GET /api/stack/health
func (s *Server) handleStackHealth(w http.ResponseWriter, r *http.Request) {
	health := config.SpecHealth{
		Validation: config.ValidationStatus{
			Status: "unknown",
		},
		Drift: config.DriftStatus{
			Status: "unknown",
		},
		Dependencies: config.DependencyStatus{
			Status: "resolved",
		},
	}

	if s.stackFile == "" {
		writeJSON(w, health)
		return
	}

	// Validation status
	_, result, err := config.ValidateStackFile(s.stackFile)
	if err != nil {
		health.Validation.Status = "errors"
		writeJSON(w, health)
		return
	}

	health.Validation.ErrorCount = result.ErrorCount
	health.Validation.WarningCount = result.WarningCount
	switch {
	case result.ErrorCount > 0:
		health.Validation.Status = "errors"
	case result.WarningCount > 0:
		health.Validation.Status = "warnings"
	default:
		health.Validation.Status = "valid"
	}

	// Drift status
	if s.stackName != "" {
		proposed, _, loadErr := config.ValidateStackFile(s.stackFile)
		if loadErr == nil {
			current := s.loadRunningSpec()
			diff := config.ComputePlan(proposed, current)
			if diff.HasChanges {
				health.Drift.Status = "drifted"
				for _, item := range diff.Items {
					switch item.Action {
					case config.DiffAdd:
						health.Drift.Added = append(health.Drift.Added, item.Name)
					case config.DiffRemove:
						health.Drift.Removed = append(health.Drift.Removed, item.Name)
					case config.DiffChange:
						health.Drift.Changed = append(health.Drift.Changed, item.Name)
					}
				}
			} else {
				health.Drift.Status = "in-sync"
			}
		}
	}

	writeJSON(w, health)
}

// handleStackSpec returns the current stack.yaml content.
// GET /api/stack/spec
func (s *Server) handleStackSpec(w http.ResponseWriter, r *http.Request) {
	if s.stackFile == "" {
		writeJSONError(w, "No stack file configured", http.StatusServiceUnavailable)
		return
	}

	data, err := os.ReadFile(s.stackFile)
	if err != nil {
		writeJSONError(w, "Failed to read stack file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"path":    s.stackFile,
		"content": string(data),
	})
}

// handleStackExport returns the current stack spec as exportable YAML.
// GET /api/stack/export
func (s *Server) handleStackExport(w http.ResponseWriter, r *http.Request) {
	if s.stackFile == "" {
		writeJSONError(w, "No stack file configured", http.StatusServiceUnavailable)
		return
	}

	stack, _, err := config.ValidateStackFile(s.stackFile)
	if err != nil {
		writeJSONError(w, "Failed to load stack: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Sanitize secrets
	sanitizeStackSecrets(stack)

	data, err := yaml.Marshal(stack)
	if err != nil {
		writeJSONError(w, "Failed to marshal stack: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"content": string(data),
		"format":  "yaml",
	})
}

// sanitizeStackSecrets replaces sensitive env values with vault placeholders.
func sanitizeStackSecrets(stack *config.Stack) {
	sensitiveKeys := []string{"PASSWORD", "SECRET", "TOKEN", "API_KEY", "APIKEY", "PRIVATE_KEY", "ACCESS_KEY", "AUTH", "CREDENTIAL"}

	sanitize := func(env map[string]string, prefix string) {
		if env == nil {
			return
		}
		for key, val := range env {
			if strings.HasPrefix(val, "${vault:") {
				continue
			}
			upper := strings.ToUpper(key)
			for _, s := range sensitiveKeys {
				if strings.Contains(upper, s) {
					env[key] = "${vault:" + prefix + "_" + key + "}"
					break
				}
			}
		}
	}

	for i := range stack.MCPServers {
		sanitize(stack.MCPServers[i].Env, stack.MCPServers[i].Name)
	}
	for i := range stack.Resources {
		sanitize(stack.Resources[i].Env, stack.Resources[i].Name)
	}
}

// handleStackSecretsMap returns which nodes reference which vault secrets.
// GET /api/stack/secrets-map
func (s *Server) handleStackSecretsMap(w http.ResponseWriter, r *http.Request) {
	if s.stackFile == "" {
		writeJSON(w, map[string]any{"secrets": map[string]any{}, "nodes": map[string]any{}})
		return
	}

	stack, _, err := config.ValidateStackFile(s.stackFile)
	if err != nil {
		writeJSONError(w, "Failed to load stack: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Build maps: secret -> nodes, node -> secrets
	secretToNodes := make(map[string][]string)
	nodeToSecrets := make(map[string][]string)

	extractVaultRefs := func(env map[string]string, nodeName string) {
		if env == nil {
			return
		}
		for _, val := range env {
			if strings.HasPrefix(val, "${vault:") && strings.HasSuffix(val, "}") && len(val) > 9 {
				secretKey := val[8 : len(val)-1]
				secretToNodes[secretKey] = appendUnique(secretToNodes[secretKey], nodeName)
				nodeToSecrets[nodeName] = appendUnique(nodeToSecrets[nodeName], secretKey)
			}
		}
	}

	for _, srv := range stack.MCPServers {
		extractVaultRefs(srv.Env, srv.Name)
	}
	for _, res := range stack.Resources {
		extractVaultRefs(res.Env, res.Name)
	}

	writeJSON(w, map[string]any{
		"secrets": secretToNodes,
		"nodes":   nodeToSecrets,
	})
}

func appendUnique(slice []string, item string) []string {
	for _, s := range slice {
		if s == item {
			return slice
		}
	}
	return append(slice, item)
}

// handleStackRecipes returns available stack recipes/templates.
// GET /api/stack/recipes
func (s *Server) handleStackRecipes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, stackRecipes)
}

// StackRecipe is a pre-built stack template.
type StackRecipe struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Spec        string `json:"spec"`
}

var stackRecipes = []StackRecipe{
	{
		ID:          "rag-pipeline",
		Name:        "RAG Pipeline",
		Description: "Retrieval-augmented generation with PostgreSQL, embeddings, and a search server",
		Category:    "ai",
		Spec: `version: "1"
name: rag-pipeline
gateway:
  auth:
    type: bearer
    token: $RAG_TOKEN
network:
  name: rag-net
  driver: bridge
mcp-servers:
  - name: embeddings
    image: ghcr.io/modelcontextprotocol/mcp-embeddings:latest
    port: 8080
    transport: http
    env:
      OPENAI_API_KEY: "${vault:OPENAI_API_KEY}"
  - name: rag-search
    image: ghcr.io/modelcontextprotocol/mcp-rag:latest
    port: 8081
    transport: http
    env:
      DATABASE_URL: "postgresql://rag:rag@postgres:5432/rag"
resources:
  - name: postgres
    image: pgvector/pgvector:pg16
    env:
      POSTGRES_USER: rag
      POSTGRES_PASSWORD: "${vault:POSTGRES_PASSWORD}"
      POSTGRES_DB: rag
    ports:
      - "5432:5432"
`,
	},
	{
		ID:          "dev-toolbox",
		Name:        "Developer Toolbox",
		Description: "File system, Git, and shell tools for coding assistants",
		Category:    "development",
		Spec: `version: "1"
name: dev-toolbox
network:
  name: dev-net
  driver: bridge
mcp-servers:
  - name: filesystem
    image: ghcr.io/modelcontextprotocol/mcp-filesystem:latest
    port: 8080
    transport: http
    env:
      ALLOWED_PATHS: /workspace
  - name: git
    image: ghcr.io/modelcontextprotocol/mcp-git:latest
    port: 8081
    transport: http
  - name: shell
    command: ["npx", "-y", "@anthropic/mcp-shell"]
    transport: stdio
`,
	},
	{
		ID:          "data-analysis",
		Name:        "Data Analysis Suite",
		Description: "SQL database, data visualization, and Python execution for analytics",
		Category:    "data",
		Spec: `version: "1"
name: data-analysis
network:
  name: data-net
  driver: bridge
mcp-servers:
  - name: sqlite
    image: ghcr.io/modelcontextprotocol/mcp-sqlite:latest
    port: 8080
    transport: http
  - name: python
    image: ghcr.io/modelcontextprotocol/mcp-python:latest
    port: 8081
    transport: http
resources:
  - name: mysql
    image: mysql:8
    env:
      MYSQL_ROOT_PASSWORD: "${vault:MYSQL_ROOT_PASSWORD}"
      MYSQL_DATABASE: analytics
    ports:
      - "3306:3306"
`,
	},
	{
		ID:          "monitoring-stack",
		Name:        "Monitoring Stack",
		Description: "Prometheus metrics, log aggregation, and alerting tools",
		Category:    "operations",
		Spec: `version: "1"
name: monitoring
network:
  name: monitoring-net
  driver: bridge
mcp-servers:
  - name: prometheus-query
    url: http://prometheus:9090
    transport: http
  - name: loki-logs
    url: http://loki:3100
    transport: http
resources:
  - name: prometheus
    image: prom/prometheus:latest
    ports:
      - "9090:9090"
  - name: loki
    image: grafana/loki:latest
    ports:
      - "3100:3100"
`,
	},
}

// handleStackAppend appends a resource to the current stack.yaml.
// POST /api/stack/append
func (s *Server) handleStackAppend(w http.ResponseWriter, r *http.Request) {
	if s.stackFile == "" {
		writeJSONError(w, "No stack file configured", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSONError(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var req struct {
		YAML         string `json:"yaml"`
		ResourceType string `json:"resourceType"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	stack, _, err := config.ValidateStackFile(s.stackFile)
	if err != nil {
		writeJSONError(w, "Failed to load stack: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var name string
	switch req.ResourceType {
	case "mcp-server":
		var res config.MCPServer
		if err := yaml.Unmarshal([]byte(req.YAML), &res); err != nil {
			writeJSONError(w, "Invalid mcp-server YAML: "+err.Error(), http.StatusBadRequest)
			return
		}
		stack.MCPServers = append(stack.MCPServers, res)
		name = res.Name
	case "resource":
		var res config.Resource
		if err := yaml.Unmarshal([]byte(req.YAML), &res); err != nil {
			writeJSONError(w, "Invalid resource YAML: "+err.Error(), http.StatusBadRequest)
			return
		}
		stack.Resources = append(stack.Resources, res)
		name = res.Name
	default:
		writeJSONError(w, "Unsupported resourceType: "+req.ResourceType, http.StatusBadRequest)
		return
	}

	out, err := yaml.Marshal(stack)
	if err != nil {
		writeJSONError(w, "Failed to marshal stack: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(s.stackFile, out, 0o644); err != nil {
		writeJSONError(w, "Failed to write stack file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"success":      true,
		"resourceType": req.ResourceType,
		"resourceName": name,
	})
}

// loadRunningSpec returns the stack config that was loaded at deploy time.
// Since the state file only stores the file path (not content), we use the
// gateway's live status as the source of truth for what's actually running.
// This returns a best-effort reconstruction from the state file's stack path.
func (s *Server) loadRunningSpec() *config.Stack {
	st, err := state.Load(s.stackName)
	if err != nil {
		return &config.Stack{Name: s.stackName}
	}

	current, _, err := config.ValidateStackFile(st.StackFile)
	if err != nil {
		return &config.Stack{Name: s.stackName}
	}
	return current
}
