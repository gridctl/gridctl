package mcp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"sort"

	"github.com/getkin/kin-openapi/openapi3"
)

// NewOpenAPIHTTPClient builds the HTTP client used to fetch specs and call
// operations, applying optional mTLS. Shared by the deployed client and the
// wizard preview so both negotiate TLS identically — a preview that trusted a
// different set of roots than deploy would mislead the operator.
func NewOpenAPIHTTPClient(certFile, keyFile, caFile string, insecureSkipVerify bool) (*http.Client, error) {
	// Clone the default transport to avoid mutating shared state.
	transport := http.DefaultTransport.(*http.Transport).Clone()

	if certFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("loading TLS client certificate: %w", err)
		}
		tlsCfg := &tls.Config{
			Certificates:       []tls.Certificate{cert},
			InsecureSkipVerify: insecureSkipVerify, //nolint:gosec // user-controlled config
		}
		if caFile != "" {
			caCert, err := os.ReadFile(caFile)
			if err != nil {
				return nil, fmt.Errorf("reading TLS CA file: %w", err)
			}
			caPool := x509.NewCertPool()
			if !caPool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("parsing TLS CA certificate: no valid certificates found in %s", caFile)
			}
			tlsCfg.RootCAs = caPool
		}
		transport.TLSClientConfig = tlsCfg
	}

	return &http.Client{Timeout: defaultOpenAPITimeout, Transport: transport}, nil
}

// SpecFetchError reports a non-2xx HTTP response while fetching a spec. It is a
// typed error so callers can classify (notably 401/403, which need different
// operator guidance than an unreachable host) without matching on message text.
type SpecFetchError struct {
	URL        string
	StatusCode int
}

func (e *SpecFetchError) Error() string {
	return fmt.Sprintf("fetching spec from %s: HTTP %d", e.URL, e.StatusCode)
}

// Skip reasons reported by EnumerateOperations. They are stable strings so the
// API layer and the web UI can key copy off them.
const (
	SkipReasonNoOperationID = "no_operation_id"
	SkipReasonUnusableName  = "unusable_tool_name"
)

// OperationSummary describes one OpenAPI operation as both the deployed client
// and the wizard preview see it.
//
// OperationID is the raw value from the spec and is what openapi.operations
// include/exclude matches against. ToolName is the sanitized form actually
// advertised over MCP. The two differ whenever an operationId contains
// characters outside [a-zA-Z0-9_-], so anything persisted into stack.yaml must
// use OperationID, never ToolName.
type OperationSummary struct {
	OperationID string
	ToolName    string
	Method      string
	Path        string
	Summary     string
	Description string
	Tags        []string
	Deprecated  bool

	// Skipped is true when this operation cannot become a tool at all.
	// SkipReason carries one of the SkipReason* constants.
	Skipped    bool
	SkipReason string

	// op is the source operation, used by RefreshTools to build the tool.
	op *openapi3.Operation
}

// EnumerateOperations walks a parsed spec and returns every operation it
// contains, including those that cannot become tools.
//
// This is the single source of truth for which operations exist and which are
// unusable. The deployed client (RefreshTools) and the wizard preview both go
// through it so their skip and sanitization rules cannot drift — a preview that
// disagreed with deploy would be worse than no preview at all.
//
// Results are sorted by path then method. kin-openapi returns paths and
// operations from maps, so without sorting the order varies between calls.
func EnumerateOperations(doc *openapi3.T) []OperationSummary {
	if doc == nil || doc.Paths == nil {
		return nil
	}

	var out []OperationSummary
	for path, pathItem := range doc.Paths.Map() {
		if pathItem == nil {
			continue
		}
		for method, op := range pathItem.Operations() {
			if op == nil {
				continue
			}

			summary := OperationSummary{
				OperationID: op.OperationID,
				Method:      method,
				Path:        path,
				Summary:     op.Summary,
				Description: op.Description,
				Tags:        op.Tags,
				Deprecated:  op.Deprecated,
				op:          op,
			}

			if op.OperationID == "" {
				summary.Skipped = true
				summary.SkipReason = SkipReasonNoOperationID
			} else {
				summary.ToolName = sanitizeOpenAPIToolName(op.OperationID)
				if summary.ToolName == "" {
					summary.Skipped = true
					summary.SkipReason = SkipReasonUnusableName
				}
			}

			out = append(out, summary)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// LoadOpenAPISpecForPreview loads and parses a spec for the wizard preview,
// without constructing a gateway client or registering anything.
//
// External references are deliberately NOT followed here, unlike the deployed
// path. Preview is reachable from the browser wizard against an arbitrary
// operator-supplied URL, so following refs inside a fetched document would let
// a hostile spec drive further daemon-side requests. The deployed path keeps
// external refs enabled because that spec is already committed to stack.yaml.
// The trade-off is that a spec whose operations live behind $ref to another
// document previews as incomplete; the operator can still enter IDs manually.
func LoadOpenAPISpecForPreview(ctx context.Context, spec string, client *http.Client) (*openapi3.T, error) {
	return loadSpecFrom(ctx, spec, client, false, false)
}
