package api

import (
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/state"

	"gopkg.in/yaml.v3"
)

// handleStack routes /api/stack/ requests.
func (s *Server) handleStack(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/stack/")
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "validate" && r.Method == http.MethodPost:
		s.handleStackValidate(w, r)
	case path == "plan" && r.Method == http.MethodGet:
		s.handleStackPlan(w, r)
	case path == "health" && r.Method == http.MethodGet:
		s.handleStackHealth(w, r)
	case path == "spec" && r.Method == http.MethodGet:
		s.handleStackSpec(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

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

	// Match CLI validation: merge aliases, expand env vars, apply defaults
	config.MergeEquippedSkills(&stack)
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
