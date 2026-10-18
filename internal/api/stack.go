package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/reload"
	"github.com/gridctl/gridctl/pkg/state"

	"gopkg.in/yaml.v3"
)

// validStackName matches names that are safe to use as filenames (alphanumeric, hyphens, underscores).
var validStackName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// writeValidationFailure writes a strict-decode or validation failure as
// issues. It returns false when err is not a ValidationErrors value.
func writeValidationFailure(w http.ResponseWriter, status int, err error) bool {
	var ve config.ValidationErrors
	if !errors.As(err, &ve) {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(config.ResultFromErrors(ve))
	return true
}

// stackEntry describes a saved stack in the library.
type stackEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// stacksDirPath returns the saved-stacks directory: the injected
// override when set (tests), the global default otherwise. An empty
// return means the home could not be resolved; ReadDir on "" then
// fails loudly rather than reading a relative path.
func (s *Server) stacksDirPath() string {
	if s.stacksDir != "" {
		return s.stacksDir
	}
	dir, err := state.StacksDir()
	if err != nil {
		return ""
	}
	return dir
}

// handleStacksList lists all saved stacks in ~/.gridctl/stacks/.
// GET /api/stacks
func (s *Server) handleStacksList(w http.ResponseWriter, r *http.Request) {
	dir := s.stacksDirPath()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, map[string]any{"stacks": []stackEntry{}})
			return
		}
		writeJSONError(w, "Failed to read stacks directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	stacks := make([]stackEntry, 0)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		stacks = append(stacks, stackEntry{
			Name: name,
			Path: filepath.Join(dir, e.Name()),
		})
	}

	writeJSON(w, map[string]any{"stacks": stacks})
}

// handleStacksSave saves a stack YAML to ~/.gridctl/stacks/<name>.yaml.
// POST /api/stacks
func (s *Server) handleStacksSave(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSONError(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var req struct {
		YAML string `json:"yaml"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		writeJSONError(w, "name is required", http.StatusBadRequest)
		return
	}
	if !validStackName.MatchString(req.Name) {
		writeJSONError(w, "invalid name: use only letters, numbers, hyphens, and underscores", http.StatusBadRequest)
		return
	}
	if req.YAML == "" {
		writeJSONError(w, "yaml is required", http.StatusBadRequest)
		return
	}

	// Validate YAML parses into a Stack
	if _, err := config.DecodeStack([]byte(req.YAML)); err != nil {
		if writeValidationFailure(w, http.StatusBadRequest, err) {
			return
		}
		writeJSONError(w, "Invalid YAML: "+err.Error(), http.StatusBadRequest)
		return
	}

	dir := s.stacksDirPath()
	if err := os.MkdirAll(dir, 0755); err != nil {
		writeJSONError(w, "Failed to create stacks directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	destPath := filepath.Join(dir, req.Name+".yaml")
	if err := os.WriteFile(destPath, []byte(req.YAML), 0644); err != nil {
		writeJSONError(w, "Failed to write stack file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"success": true,
		"path":    destPath,
		"name":    req.Name,
	})
}

// handleStackInitialize cold-loads a named stack into a running stackless daemon.
// POST /api/stack/initialize
func (s *Server) handleStackInitialize(w http.ResponseWriter, r *http.Request) {
	if s.stackFile != "" {
		writeJSONError(w, "A stack is already loaded; use reload for subsequent changes", http.StatusConflict)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSONError(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		writeJSONError(w, "name is required", http.StatusBadRequest)
		return
	}

	stackPath := filepath.Join(s.stacksDirPath(), req.Name+".yaml")
	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		writeJSONError(w, "Stack not found: "+req.Name, http.StatusNotFound)
		return
	}

	watching := false

	if handler := s.ReloadHandler(); handler != nil {
		result, err := handler.Initialize(r.Context(), stackPath)
		if err != nil {
			writeJSONError(w, "Failed to initialize stack: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if !result.Success {
			if writePreflightFailure(w, result) {
				return
			}
			// Include per-item errors in the body so the wizard can surface
			// individual server registration failures instead of a single
			// opaque message.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":  "Stack initialization failed: " + result.Message,
				"errors": result.Errors,
			})
			return
		}

		// Start file watcher if a callback was provided
		if s.startWatcher != nil {
			s.startWatcher(stackPath)
			watching = true
		}
	} else {
		writeJSONError(w, "Stack initialization is not ready; startup security preflight is unavailable", http.StatusServiceUnavailable)
		return
	}

	// Persist stack file and name on the server so /ready and other endpoints work
	s.stackFile = stackPath
	s.stackName = req.Name

	writeJSON(w, map[string]any{
		"success":  true,
		"name":     req.Name,
		"watching": watching,
	})
}

// handleStackValidate validates a stack YAML body.
// POST /api/stack/validate
func (s *Server) handleStackValidate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		writeJSONError(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	stack, err := config.DecodeStack(body)
	if err != nil {
		var ve config.ValidationErrors
		if errors.As(err, &ve) {
			writeJSON(w, config.ResultFromErrors(ve))
			return
		}
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
	if err := config.ExpandStackVarsWithEnvChecked(stack); err != nil {
		writeJSON(w, &config.ValidationResult{
			Valid:      false,
			ErrorCount: 1,
			Issues: []config.ValidationIssue{{
				Field: "variables", Message: err.Error(), Severity: config.SeverityError,
			}},
		})
		return
	}
	stack.SetDefaults()
	result := config.ValidateWithIssues(stack)
	writeJSON(w, result)
}

// handleStackPlan compares the on-disk stack against the configuration the
// daemon loaded, when a reload handler is present. Otherwise it compares the
// file against the path recorded in daemon state.
// GET /api/stack/plan
func (s *Server) handleStackPlan(w http.ResponseWriter, r *http.Request) {
	if s.stackFile == "" || s.stackName == "" {
		writeJSONError(w, "No stack is currently deployed", http.StatusServiceUnavailable)
		return
	}

	diff, err := s.planAgainstRunning(r.Context())
	if err != nil {
		if errors.Is(err, reload.ErrNoStackLoaded) {
			writeJSONError(w, "No stack is currently deployed", http.StatusServiceUnavailable)
			return
		}
		writeJSONError(w, "Failed to load stack file: "+err.Error(), http.StatusInternalServerError)
		return
	}
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
	_, result, err := config.ValidateStackFile(s.stackFile, s.overrideLoadOptions()...)
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

	// Drift status. With a reload handler this is the daemon's loaded
	// configuration, not a second parse of the stack file. A failed
	// comparison leaves drift unknown rather than reporting in-sync.
	if s.stackName != "" {
		diff, planErr := s.planAgainstRunning(r.Context())
		if planErr == nil && diff != nil {
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

	// Per-replica health for servers with replicas > 1. Single-replica servers
	// are intentionally omitted so the response shape is unchanged when no
	// server is configured for horizontal scaling.
	health.Replicas = s.collectReplicaHealth()
	if result != nil && result.Override != "" {
		health.Override = result.Override
	} else if path := s.effectiveOverridePath(); path != "" {
		health.Override = path
	}

	writeJSON(w, health)
}

// collectReplicaHealth returns per-replica health for every registered MCP
// server with more than one replica. Returns nil when the gateway is absent
// or no multi-replica servers exist.
func (s *Server) collectReplicaHealth() map[string][]config.ReplicaHealth {
	if s.gateway == nil {
		return nil
	}
	var out map[string][]config.ReplicaHealth
	now := time.Now()
	for _, st := range s.gateway.Status() {
		if len(st.Replicas) <= 1 {
			continue
		}
		healths := make([]config.ReplicaHealth, 0, len(st.Replicas))
		for _, r := range st.Replicas {
			healths = append(healths, toReplicaHealth(r, now))
		}
		if out == nil {
			out = make(map[string][]config.ReplicaHealth)
		}
		out[st.Name] = healths
	}
	return out
}

// toReplicaHealth projects an mcp.ReplicaStatus to the API-facing
// config.ReplicaHealth shape.
func toReplicaHealth(r mcp.ReplicaStatus, now time.Time) config.ReplicaHealth {
	h := config.ReplicaHealth{
		Execution:         r.Execution,
		ReplicaID:         r.ReplicaID,
		State:             r.State,
		InFlight:          r.InFlight,
		LastError:         r.LastError,
		RestartAttempts:   r.RestartAttempts,
		PID:               r.PID,
		ContainerID:       r.ContainerID,
		RestartPolicy:     r.RestartPolicy,
		RestartExhausted:  r.RestartExhausted,
		ContainerRestarts: r.ContainerRestarts,
	}
	if !r.StartedAt.IsZero() && r.Healthy {
		if d := now.Sub(r.StartedAt); d > 0 {
			h.UptimeSeconds = int64(d / time.Second)
		}
	}
	if r.NextRetryAt != nil && !r.NextRetryAt.IsZero() {
		if d := r.NextRetryAt.Sub(now); d > 0 {
			h.NextRetrySeconds = int64(d / time.Second)
		}
	}
	return h
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

	stack, _, err := config.ExportStack(r.Context(), s.stackFile)
	if err != nil {
		writeJSONError(w, "Failed to load stack: "+err.Error(), http.StatusInternalServerError)
		return
	}

	data, err := yaml.Marshal(stack)
	if err != nil {
		writeJSONError(w, "Failed to encode stack export", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"content": string(data),
		"format":  "yaml",
		"notice":  config.ExportNotices(stack),
	})
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

// handleStackAppend appends a resource to the current stack.yaml. The
// persistence path uses the same lock + hash + atomic-write pattern as
// setServerTools so concurrent callers serialize, external edits between read
// and write are detected (HTTP 409), and a mid-write crash leaves the
// original file intact. The yaml.Node round-trip in patchAppendResource keeps
// hand-written comments and key ordering — the previous implementation
// re-emitted from a Go struct and silently destroyed both.
//
// Resource-name uniqueness is not enforced here; that is the validator's
// concern and unchanged from prior behavior.
//
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

	// Validate resourceType + snippet up front. Bad input never touches disk
	// or takes the per-path lock.
	var (
		resourceName string
		newServer    config.MCPServer
		newResource  config.Resource
	)
	switch req.ResourceType {
	case "mcp-server":
		decoded, decErr := config.DecodeMCPServer([]byte(req.YAML))
		if decErr != nil {
			if writeValidationFailure(w, http.StatusBadRequest, decErr) {
				return
			}
			writeJSONError(w, "Invalid mcp-server YAML: "+decErr.Error(), http.StatusBadRequest)
			return
		}
		newServer = *decoded
		if newServer.Name == "" {
			writeJSONError(w, "mcp-server name is required", http.StatusBadRequest)
			return
		}
		resourceName = newServer.Name
	case "resource":
		decoded, decErr := config.DecodeResource([]byte(req.YAML))
		if decErr != nil {
			if writeValidationFailure(w, http.StatusBadRequest, decErr) {
				return
			}
			writeJSONError(w, "Invalid resource YAML: "+decErr.Error(), http.StatusBadRequest)
			return
		}
		newResource = *decoded
		if newResource.Name == "" {
			writeJSONError(w, "resource name is required", http.StatusBadRequest)
			return
		}
		resourceName = newResource.Name
	default:
		writeJSONError(w, "Unsupported resourceType: "+req.ResourceType, http.StatusBadRequest)
		return
	}

	mu := stackFileLock(s.stackFile)
	mu.Lock()
	defer mu.Unlock()

	original, err := os.ReadFile(s.stackFile)
	if err != nil {
		writeJSONError(w, "Failed to read stack file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	originalHash := sha256.Sum256(original)

	// Validate the post-append shape against the typed schema. We mutate a
	// copy of the parsed struct so the YAML bytes the user wrote do not need
	// to round-trip through canonical encoding for validation to run.
	stack, err := config.DecodeStack(original)
	if err != nil {
		if writeValidationFailure(w, http.StatusBadRequest, err) {
			return
		}
		writeJSONError(w, "Failed to parse stack: "+err.Error(), http.StatusInternalServerError)
		return
	}
	switch req.ResourceType {
	case "mcp-server":
		stack.MCPServers = append(stack.MCPServers, newServer)
	case "resource":
		stack.Resources = append(stack.Resources, newResource)
	}
	if err := config.ExpandStackVarsWithEnvChecked(stack); err != nil {
		writeJSONError(w, "Stack variable resolution failed after append: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}
	stack.SetDefaults()
	if result := config.ValidateWithIssues(stack); !result.Valid {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":      "stack validation failed after append",
			"validation": result,
		})
		return
	}

	updated, err := patchAppendResource(original, req.ResourceType, []byte(req.YAML))
	if err != nil {
		writeJSONError(w, "Failed to patch stack: "+err.Error(), http.StatusInternalServerError)
		return
	}

	fireBetweenReadsHook()

	// Re-read right before write to catch any external edit that landed in
	// the window between our initial read and the rename. With the per-path
	// mutex this is a tight window, but external editors (vim, git) do not
	// respect our lock.
	current, err := os.ReadFile(s.stackFile)
	if err != nil {
		writeJSONError(w, "Failed to re-read stack file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if sha256.Sum256(current) != originalHash {
		writeJSONError(w, "stack file was modified on disk since read; reload before retrying", http.StatusConflict)
		return
	}

	if err := atomicWrite(s.stackFile, updated); err != nil {
		writeJSONError(w, "Failed to write stack file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"success":      true,
		"resourceType": req.ResourceType,
		"resourceName": resourceName,
	})
}

// planAgainstRunning compares the daemon's stack file with the configuration
// it loaded. Without a reload handler it keeps the file-based comparison so
// tests that construct a Server directly still pass. It does not fall back
// to that parse when the handler is present but cannot plan.
func (s *Server) planAgainstRunning(ctx context.Context) (*config.PlanDiff, error) {
	if h := s.ReloadHandler(); h != nil {
		return h.Plan(ctx)
	}
	proposed, _, err := config.ValidateStackFile(s.stackFile, s.overrideLoadOptions()...)
	if err != nil {
		return nil, err
	}
	return config.ComputePlan(proposed, s.loadRunningSpec()), nil
}

// loadRunningSpec parses the stack file recorded in the daemon state.
// It does not read the gateway's live status. Plan and health no longer
// rely on it when a reload handler is present; variable usage still does.
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
