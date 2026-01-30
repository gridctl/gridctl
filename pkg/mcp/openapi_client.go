package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

// Default HTTP timeout for OpenAPI requests
const defaultOpenAPITimeout = 30 * time.Second

// Maximum response body size (10MB) to prevent memory exhaustion
const maxResponseBodySize = 10 * 1024 * 1024

// OpenAPIClient implements AgentClient by transforming OpenAPI operations to MCP tools.
// It parses an OpenAPI specification and converts each operation into an MCP tool,
// proxying tool calls to HTTP requests against the target API.
type OpenAPIClient struct {
	name       string
	spec       string
	baseURL    string
	authType   string
	authToken  string
	authHeader string
	authValue  string
	includeOps map[string]bool
	excludeOps map[string]bool
	httpClient *http.Client

	mu            sync.RWMutex
	initialized   bool
	tools         []Tool
	operations    map[string]*OpenAPIOperation // toolName -> operation
	serverInfo    ServerInfo
	toolWhitelist []string
	cachedDoc     *openapi3.T // Cached OpenAPI document
}

// OpenAPIOperation holds parsed OpenAPI operation details for execution.
type OpenAPIOperation struct {
	Method       string
	Path         string
	PathParams   []string // Parameter names in path order (always required)
	QueryParams  map[string]*openapi3.Parameter
	HeaderParams map[string]*openapi3.Parameter
	RequestBody  *openapi3.RequestBodyRef
}

// NewOpenAPIClient creates an OpenAPI-based MCP client.
func NewOpenAPIClient(name string, cfg *OpenAPIClientConfig) (*OpenAPIClient, error) {
	c := &OpenAPIClient{
		name:       name,
		spec:       cfg.Spec,
		baseURL:    cfg.BaseURL,
		authType:   cfg.AuthType,
		authToken:  cfg.AuthToken,
		authHeader: cfg.AuthHeader,
		authValue:  cfg.AuthValue,
		httpClient: &http.Client{Timeout: defaultOpenAPITimeout},
		operations: make(map[string]*OpenAPIOperation),
	}

	if len(cfg.Include) > 0 {
		c.includeOps = make(map[string]bool)
		for _, op := range cfg.Include {
			c.includeOps[op] = true
		}
	}
	if len(cfg.Exclude) > 0 {
		c.excludeOps = make(map[string]bool)
		for _, op := range cfg.Exclude {
			c.excludeOps[op] = true
		}
	}

	return c, nil
}

// Name returns the client name.
func (c *OpenAPIClient) Name() string {
	return c.name
}

// SetToolWhitelist sets the list of allowed tool names.
func (c *OpenAPIClient) SetToolWhitelist(tools []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.toolWhitelist = tools
}

// Initialize loads and parses the OpenAPI spec.
func (c *OpenAPIClient) Initialize(ctx context.Context) error {
	doc, err := c.loadSpec(ctx)
	if err != nil {
		return fmt.Errorf("loading OpenAPI spec: %w", err)
	}

	// Validate the spec
	if err := doc.Validate(ctx); err != nil {
		return fmt.Errorf("validating OpenAPI spec: %w", err)
	}

	// Determine base URL from spec if not overridden
	if c.baseURL == "" && len(doc.Servers) > 0 {
		c.baseURL = doc.Servers[0].URL
	}

	// Validate that we have a base URL
	if c.baseURL == "" {
		return fmt.Errorf("no base URL: either configure baseUrl or ensure the OpenAPI spec has a servers entry")
	}

	c.mu.Lock()
	c.initialized = true
	c.cachedDoc = doc
	c.serverInfo = ServerInfo{
		Name:    doc.Info.Title,
		Version: doc.Info.Version,
	}
	c.mu.Unlock()

	return nil
}

// RefreshTools builds MCP tools from OpenAPI operations.
func (c *OpenAPIClient) RefreshTools(ctx context.Context) error {
	// Use cached doc if available, otherwise load fresh
	c.mu.RLock()
	doc := c.cachedDoc
	whitelist := c.toolWhitelist
	c.mu.RUnlock()

	if doc == nil {
		var err error
		doc, err = c.loadSpec(ctx)
		if err != nil {
			return err
		}
	}

	var tools []Tool
	operations := make(map[string]*OpenAPIOperation)

	if doc.Paths == nil {
		c.mu.Lock()
		c.tools = tools
		c.operations = operations
		c.mu.Unlock()
		return nil
	}

	// Build whitelist map for O(1) lookup
	whitelistMap := make(map[string]bool)
	for _, name := range whitelist {
		whitelistMap[name] = true
	}

	for path, pathItem := range doc.Paths.Map() {
		if pathItem == nil {
			continue
		}
		for method, op := range pathItem.Operations() {
			if op == nil || op.OperationID == "" {
				continue // Skip operations without ID
			}

			// Apply include/exclude filters
			if !c.shouldInclude(op.OperationID) {
				continue
			}

			// Convert to MCP tool
			tool, operation := c.operationToTool(method, path, op)

			// Handle empty tool name (operationID was all invalid chars)
			if tool.Name == "" {
				continue
			}

			// Apply whitelist filter (using pre-built map)
			if len(whitelistMap) > 0 && !whitelistMap[tool.Name] {
				continue
			}

			tools = append(tools, tool)
			operations[tool.Name] = operation
		}
	}

	c.mu.Lock()
	c.tools = tools
	c.operations = operations
	c.mu.Unlock()

	return nil
}

// Tools returns the cached tools.
func (c *OpenAPIClient) Tools() []Tool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tools
}

// CallTool executes an OpenAPI operation.
func (c *OpenAPIClient) CallTool(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
	c.mu.RLock()
	op, ok := c.operations[name]
	c.mu.RUnlock()

	if !ok {
		return &ToolCallResult{
			Content: []Content{NewTextContent(fmt.Sprintf("unknown tool: %s", name))},
			IsError: true,
		}, nil
	}

	// Validate required path parameters are present
	for _, paramName := range op.PathParams {
		if _, ok := args[paramName]; !ok {
			return &ToolCallResult{
				Content: []Content{NewTextContent(fmt.Sprintf("missing required path parameter: %s", paramName))},
				IsError: true,
			}, nil
		}
	}

	// Build and execute HTTP request
	resp, statusCode, err := c.executeOperation(ctx, op, args)
	if err != nil {
		return &ToolCallResult{
			Content: []Content{NewTextContent(fmt.Sprintf("error: %v", err))},
			IsError: true,
		}, nil
	}

	// Check for error status codes
	if statusCode >= 400 {
		return &ToolCallResult{
			Content: []Content{NewTextContent(fmt.Sprintf("HTTP %d: %s", statusCode, resp))},
			IsError: true,
		}, nil
	}

	return &ToolCallResult{
		Content: []Content{NewTextContent(resp)},
	}, nil
}

// IsInitialized returns whether the client has been initialized.
func (c *OpenAPIClient) IsInitialized() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.initialized
}

// ServerInfo returns the server information.
func (c *OpenAPIClient) ServerInfo() ServerInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.serverInfo
}

// loadSpec loads the OpenAPI spec from URL or file.
func (c *OpenAPIClient) loadSpec(ctx context.Context) (*openapi3.T, error) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	loader.Context = ctx // Propagate context for cancellation

	if strings.HasPrefix(c.spec, "http://") || strings.HasPrefix(c.spec, "https://") {
		u, err := url.Parse(c.spec)
		if err != nil {
			return nil, fmt.Errorf("parsing spec URL: %w", err)
		}
		return loader.LoadFromURI(u)
	}

	// Load from file
	data, err := os.ReadFile(c.spec)
	if err != nil {
		return nil, fmt.Errorf("reading spec file: %w", err)
	}

	return loader.LoadFromData(data)
}

// shouldInclude checks if an operation should be included based on filters.
func (c *OpenAPIClient) shouldInclude(operationID string) bool {
	// If include list is set, operation must be in it
	if len(c.includeOps) > 0 {
		return c.includeOps[operationID]
	}
	// If exclude list is set, operation must not be in it
	if len(c.excludeOps) > 0 {
		return !c.excludeOps[operationID]
	}
	// No filters - include all
	return true
}

// operationToTool converts an OpenAPI operation to an MCP tool.
func (c *OpenAPIClient) operationToTool(method, path string, op *openapi3.Operation) (Tool, *OpenAPIOperation) {
	pathParams := extractPathParams(path)
	properties, required := c.buildParameterSchema(op, pathParams)
	operation := c.buildOperation(method, path, pathParams, op)

	// Path parameters are always required in OpenAPI
	for _, p := range pathParams {
		if !contains(required, p) {
			required = append(required, p)
		}
	}

	// Build input schema
	inputSchema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		inputSchema["required"] = required
	}

	// Marshal is safe here - inputSchema contains only primitives
	inputSchemaBytes, _ := json.Marshal(inputSchema)

	description := buildDescription(op)

	return Tool{
		Name:        sanitizeOpenAPIToolName(op.OperationID),
		Description: description,
		InputSchema: inputSchemaBytes,
	}, operation
}

// extractPathParams extracts parameter names from a URL path template.
func extractPathParams(path string) []string {
	pathParamRegex := regexp.MustCompile(`\{([^}]+)\}`)
	matches := pathParamRegex.FindAllStringSubmatch(path, -1)
	params := make([]string, 0, len(matches))
	for _, match := range matches {
		params = append(params, match[1])
	}
	return params
}

// buildParameterSchema builds the JSON Schema properties and required list from operation parameters.
func (c *OpenAPIClient) buildParameterSchema(op *openapi3.Operation, pathParams []string) (map[string]any, []string) {
	properties := make(map[string]any)
	var required []string

	// Process parameters
	for _, paramRef := range op.Parameters {
		if paramRef == nil || paramRef.Value == nil {
			continue
		}
		param := paramRef.Value

		// Convert parameter schema to JSON Schema property
		prop := c.parameterToProperty(param)
		properties[param.Name] = prop

		if param.Required {
			required = append(required, param.Name)
		}
	}

	// Process request body
	if op.RequestBody != nil && op.RequestBody.Value != nil {
		rb := op.RequestBody.Value
		// Look for JSON content type
		if content, ok := rb.Content["application/json"]; ok && content.Schema != nil {
			bodySchema := c.schemaToJSONSchema(content.Schema)
			properties["body"] = bodySchema

			if rb.Required {
				required = append(required, "body")
			}
		}
	}

	return properties, required
}

// buildOperation creates the operation struct from an OpenAPI operation.
func (c *OpenAPIClient) buildOperation(method, path string, pathParams []string, op *openapi3.Operation) *OpenAPIOperation {
	operation := &OpenAPIOperation{
		Method:       method,
		Path:         path,
		PathParams:   pathParams,
		QueryParams:  make(map[string]*openapi3.Parameter),
		HeaderParams: make(map[string]*openapi3.Parameter),
		RequestBody:  op.RequestBody,
	}

	// Store parameter info for execution
	for _, paramRef := range op.Parameters {
		if paramRef == nil || paramRef.Value == nil {
			continue
		}
		param := paramRef.Value
		switch param.In {
		case "query":
			operation.QueryParams[param.Name] = param
		case "header":
			operation.HeaderParams[param.Name] = param
		}
	}

	return operation
}

// buildDescription creates a description from operation summary and description.
func buildDescription(op *openapi3.Operation) string {
	description := op.Summary
	if op.Description != "" {
		if description != "" {
			description += ": " + op.Description
		} else {
			description = op.Description
		}
	}
	return description
}

// parameterToProperty converts an OpenAPI parameter to a JSON Schema property.
func (c *OpenAPIClient) parameterToProperty(param *openapi3.Parameter) map[string]any {
	prop := make(map[string]any)

	if param.Schema != nil && param.Schema.Value != nil {
		schema := param.Schema.Value
		if schema.Type != nil && len(*schema.Type) > 0 {
			prop["type"] = (*schema.Type)[0]
		}
		if schema.Description != "" {
			prop["description"] = schema.Description
		} else if param.Description != "" {
			prop["description"] = param.Description
		}
		if len(schema.Enum) > 0 {
			prop["enum"] = schema.Enum
		}
		if schema.Default != nil {
			prop["default"] = schema.Default
		}
	} else if param.Description != "" {
		prop["description"] = param.Description
		prop["type"] = "string" // Default to string if no schema
	}

	return prop
}

// schemaToJSONSchema converts an OpenAPI schema to a JSON Schema object.
func (c *OpenAPIClient) schemaToJSONSchema(schemaRef *openapi3.SchemaRef) map[string]any {
	if schemaRef == nil || schemaRef.Value == nil {
		return map[string]any{"type": "object"}
	}

	schema := schemaRef.Value
	result := make(map[string]any)

	// Handle type
	if schema.Type != nil && len(*schema.Type) > 0 {
		result["type"] = (*schema.Type)[0]
	}

	// Handle description
	if schema.Description != "" {
		result["description"] = schema.Description
	}

	// Handle properties for objects
	if len(schema.Properties) > 0 {
		props := make(map[string]any)
		for name, propRef := range schema.Properties {
			props[name] = c.schemaToJSONSchema(propRef)
		}
		result["properties"] = props
	}

	// Handle required fields
	if len(schema.Required) > 0 {
		result["required"] = schema.Required
	}

	// Handle array items
	if schema.Items != nil {
		result["items"] = c.schemaToJSONSchema(schema.Items)
	}

	// Handle enum
	if len(schema.Enum) > 0 {
		result["enum"] = schema.Enum
	}

	return result
}

// executeOperation executes an HTTP request for the given operation.
func (c *OpenAPIClient) executeOperation(ctx context.Context, op *OpenAPIOperation, args map[string]any) (string, int, error) {
	// Build URL with path parameters substituted
	path := op.Path
	for _, paramName := range op.PathParams {
		if val, ok := args[paramName]; ok {
			// URL-encode the value to prevent injection
			encoded := url.PathEscape(fmt.Sprintf("%v", val))
			path = strings.Replace(path, "{"+paramName+"}", encoded, 1)
		}
	}

	// Verify all path parameters were substituted
	if strings.Contains(path, "{") {
		return "", 0, fmt.Errorf("unsubstituted path parameters in: %s", path)
	}

	// Build query string
	query := url.Values{}
	for paramName := range op.QueryParams {
		if val, ok := args[paramName]; ok {
			query.Set(paramName, fmt.Sprintf("%v", val))
		}
	}

	// Construct full URL
	fullURL := strings.TrimSuffix(c.baseURL, "/") + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	// Build request body
	var bodyReader io.Reader
	if body, ok := args["body"]; ok {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return "", 0, fmt.Errorf("marshaling request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(op.Method), fullURL, bodyReader)
	if err != nil {
		return "", 0, fmt.Errorf("creating request: %w", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/json")
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Add header parameters
	for paramName := range op.HeaderParams {
		if val, ok := args[paramName]; ok {
			req.Header.Set(paramName, fmt.Sprintf("%v", val))
		}
	}

	// Apply authentication
	c.applyAuth(req)

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body with size limit to prevent memory exhaustion
	limitedReader := io.LimitReader(resp.Body, maxResponseBodySize)
	respBody, err := io.ReadAll(limitedReader)
	if err != nil {
		return "", resp.StatusCode, fmt.Errorf("reading response: %w", err)
	}

	return string(respBody), resp.StatusCode, nil
}

// applyAuth applies authentication headers to the request.
func (c *OpenAPIClient) applyAuth(req *http.Request) {
	switch c.authType {
	case "bearer":
		if c.authToken != "" {
			req.Header.Set("Authorization", "Bearer "+c.authToken)
		}
	case "header":
		if c.authHeader != "" && c.authValue != "" {
			req.Header.Set(c.authHeader, c.authValue)
		}
	}
}

// sanitizeOpenAPIToolName ensures the tool name is valid for MCP.
// MCP tool names should match: ^[a-zA-Z0-9_-]{1,64}$
func sanitizeOpenAPIToolName(name string) string {
	// Replace invalid characters with underscores
	result := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, name)

	// Truncate if too long
	if len(result) > 64 {
		result = result[:64]
	}

	// Handle empty result (operationID was all invalid chars)
	if result == "" || result == strings.Repeat("_", len(result)) {
		return ""
	}

	return result
}

// contains checks if a slice contains a string.
func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
