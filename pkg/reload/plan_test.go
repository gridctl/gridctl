package reload

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
)

func TestHandler_Plan_DiffsAgainstLoadedConfig(t *testing.T) {
	path := writeStackFile(t, `
name: plan-loaded
mcp-servers:
  - name: a
    image: alpine
    port: 3000
`)
	current, err := config.LoadStack(path, config.WithQuietLoad())
	if err != nil {
		t.Fatalf("load current: %v", err)
	}
	h, mockRT := setupHandler(t, path, current)

	started := 0
	mockRT.startFn = func(ctx context.Context, cfg runtime.WorkloadConfig) (*runtime.WorkloadStatus, error) {
		started++
		return nil, errors.New("plan must not start workloads")
	}
	registered := 0
	h.SetRegisterServerFunc(func(ctx context.Context, server config.MCPServer, replicas []ReplicaRuntime, stackPath string) error {
		registered++
		return errors.New("plan must not register servers")
	})
	applied := 0
	h.SetOnConfigApplied(func(*config.Stack) { applied++ })
	preflight := 0
	h.SetSecurityPreflight(func(*config.Stack) []string {
		preflight++
		return []string{"bind"}
	})

	updated := `
name: plan-loaded
mcp-servers:
  - name: a
    image: alpine
    port: 3000
  - name: b
    image: alpine
    port: 3001
`
	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		t.Fatal(err)
	}

	diff, err := h.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if diff == nil || !diff.HasChanges {
		t.Fatalf("diff = %+v, want changes", diff)
	}
	if len(diff.Items) != 1 {
		t.Fatalf("items = %+v, want one add", diff.Items)
	}
	item := diff.Items[0]
	if item.Action != config.DiffAdd || item.Kind != "mcp-server" || item.Name != "b" {
		t.Fatalf("item = %+v, want add mcp-server b", item)
	}
	for _, item := range diff.Items {
		if item.Kind == "network" {
			t.Fatalf("unexpected network item: %+v", item)
		}
	}
	if started != 0 || registered != 0 || applied != 0 || preflight != 0 {
		t.Fatalf("plan performed work: started=%d registered=%d applied=%d preflight=%d", started, registered, applied, preflight)
	}
	got := h.CurrentConfig()
	if got != current || len(got.MCPServers) != 1 || got.MCPServers[0].Name != "a" {
		t.Fatalf("CurrentConfig changed: %+v", got)
	}
}

func TestHandler_Plan_NoStackLoaded(t *testing.T) {
	h := NewHandler("", nil, mcp.NewGateway(), runtime.NewOrchestrator(nil, nil), 8180, 9000, nil, nil)
	diff, err := h.Plan(context.Background())
	if !errors.Is(err, ErrNoStackLoaded) {
		t.Fatalf("err = %v, want ErrNoStackLoaded", err)
	}
	if diff != nil {
		t.Fatalf("diff = %+v, want nil", diff)
	}
}

func TestHandler_Plan_InvalidCandidate(t *testing.T) {
	path := writeStackFile(t, ":::not yaml")
	current := &config.Stack{Name: "kept", MCPServers: []config.MCPServer{{Name: "a", Image: "alpine", Port: 3000}}}
	h := NewHandler(path, current, nil, nil, 8180, 9000, nil, nil)

	diff, err := h.Plan(context.Background())
	if err == nil {
		t.Fatal("expected candidate load error")
	}
	if errors.Is(err, ErrNoStackLoaded) {
		t.Fatalf("parse failure reported as no stack: %v", err)
	}
	if diff != nil {
		t.Fatalf("diff = %+v, want nil", diff)
	}
	if h.CurrentConfig() != current {
		t.Fatal("CurrentConfig changed after a failed plan")
	}
}

func TestHandler_Plan_HonorsCanceledContext(t *testing.T) {
	path := writeStackFile(t, ":::not yaml")
	current := &config.Stack{Name: "kept"}
	h := NewHandler(path, current, nil, nil, 8180, 9000, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	diff, err := h.Plan(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if diff != nil {
		t.Fatalf("diff = %+v, want nil", diff)
	}
	if h.CurrentConfig() != current {
		t.Fatal("CurrentConfig changed")
	}
}

func TestHandler_Plan_RedactsStoreBackedFields(t *testing.T) {
	// URL servers reject a network field, so url and network are separate
	// items. Both must be redacted when the field is a store consumer.
	path := writeStackFile(t, `
name: redact
mcp-servers:
  - name: s
    url: https://old.example
    transport: http
  - name: netted
    image: alpine
    port: 3000
    network: old-net
  - name: img
    image: old-img:1
    port: 3001
  - name: plain
    image: alpine:1
    port: 3002
resources:
  - name: db
    image: old-image:1
    network: old-res-net
`)
	current, err := config.LoadStack(path, config.WithQuietLoad())
	if err != nil {
		t.Fatalf("load current: %v", err)
	}
	current.References = config.ReferenceIndex{
		"KEY":  {{Kind: config.RefKindMCPServer, Name: "s", Field: "url"}},
		"NET":  {{Kind: config.RefKindMCPServer, Name: "netted", Field: "network"}},
		"IMG":  {{Kind: config.RefKindMCPServer, Name: "img", Field: "image"}},
		"RIMG": {{Kind: config.RefKindResource, Name: "db", Field: "image"}},
		"RNET": {{Kind: config.RefKindResource, Name: "db", Field: "network"}},
	}
	h := NewHandler(path, current, nil, nil, 8180, 9000, nil, nil)

	updated := `
name: redact
mcp-servers:
  - name: s
    url: https://new.example
    transport: http
  - name: netted
    image: alpine
    port: 3000
    network: new-net
  - name: img
    image: new-img:1
    port: 3001
  - name: plain
    image: alpine:2
    port: 3002
resources:
  - name: db
    image: new-image:1
    network: new-res-net
`
	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		t.Fatal(err)
	}

	diff, err := h.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	joined := planDetails(diff)
	for _, secret := range []string{"old.example", "new.example", "old-net", "new-net", "old-img", "new-img", "old-image", "new-image", "old-res-net", "new-res-net"} {
		if strings.Contains(joined, secret) {
			t.Errorf("plan details contain %q:\n%s", secret, joined)
		}
	}

	urlItem := planItem(t, diff, "s")
	if !detailExact(urlItem.Details, "url changed") {
		t.Fatalf("url details = %#v, want %q", urlItem.Details, "url changed")
	}
	netItem := planItem(t, diff, "netted")
	if !detailExact(netItem.Details, "network changed") {
		t.Fatalf("network details = %#v, want %q", netItem.Details, "network changed")
	}
	imgItem := planItem(t, diff, "img")
	if imgItem.Kind != "mcp-server" || !detailExact(imgItem.Details, "image changed") {
		t.Fatalf("image item = %+v, want mcp-server image changed", imgItem)
	}
	resItem := planItem(t, diff, "db")
	if resItem.Kind != "resource" || !detailExact(resItem.Details, "image changed") || !detailExact(resItem.Details, "network changed") {
		t.Fatalf("resource item = %+v, want image changed and network changed", resItem)
	}

	plain := planItem(t, diff, "plain")
	foundLiteral := false
	for _, detail := range plain.Details {
		if strings.Contains(detail, "alpine:1") && strings.Contains(detail, "alpine:2") {
			foundLiteral = true
		}
		if detail == "image changed" {
			t.Fatalf("literal image was redacted: %#v", plain.Details)
		}
	}
	if !foundLiteral {
		t.Fatalf("literal image details = %#v, want old and new tags", plain.Details)
	}
	if h.CurrentConfig() != current {
		t.Fatal("CurrentConfig changed")
	}
}

func TestHandler_Plan_MissingVariableNamesOnly(t *testing.T) {
	path := writeStackFile(t, `
name: missing-var
mcp-servers:
  - name: s
    url: ${var:SECRET_KEY}
    transport: http
`)
	current := &config.Stack{
		Name: "missing-var",
		MCPServers: []config.MCPServer{{
			Name: "s",
			URL:  "https://loaded-secret.example",
		}},
	}
	vault := mapVault{values: map[string]string{"OTHER": "super-secret-value"}}
	h := NewHandler(path, current, nil, nil, 8180, 9000, vault, nil)

	_, err := h.Plan(context.Background())
	if err == nil {
		t.Fatal("expected missing variable error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "missing variable(s): SECRET_KEY") {
		t.Fatalf("error = %q, want missing variable name", msg)
	}
	if strings.Contains(msg, "super-secret-value") || strings.Contains(msg, "loaded-secret.example") {
		t.Fatalf("error contains a resolved value: %q", msg)
	}
	if h.CurrentConfig() != current {
		t.Fatal("CurrentConfig changed")
	}
}

type mapVault struct {
	values map[string]string
}

func (m mapVault) Get(key string) (string, bool) {
	v, ok := m.values[key]
	return v, ok
}

func planItem(t *testing.T, diff *config.PlanDiff, name string) config.DiffItem {
	t.Helper()
	if diff == nil {
		t.Fatal("nil diff")
	}
	for _, item := range diff.Items {
		if item.Name == name {
			return item
		}
	}
	t.Fatalf("no item %q in %+v", name, diff.Items)
	return config.DiffItem{}
}

func planDetails(diff *config.PlanDiff) string {
	if diff == nil {
		return ""
	}
	var b strings.Builder
	for _, item := range diff.Items {
		for _, detail := range item.Details {
			b.WriteString(detail)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func detailExact(details []string, want string) bool {
	for _, detail := range details {
		if detail == want {
			return true
		}
	}
	return false
}
