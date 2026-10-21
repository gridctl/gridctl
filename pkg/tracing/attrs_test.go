package tracing

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestLegacyAliases_ExactOldKeys(t *testing.T) {
	want := map[attribute.Key][]attribute.Key{
		AttrServerName:         {AttrLegacyServerName, AttrLegacyMCPServerName, AttrLegacyMCPServer},
		AttrReplicaID:          {AttrLegacyReplicaID},
		AttrClientName:         {AttrLegacyClientName},
		AttrProtocolGeneration: {AttrLegacyProtocolGeneration},
		AttrGenAIToolName:      {AttrLegacyToolName, AttrLegacyBareToolName},
		AttrOutputFormat:       {AttrLegacyOutputFormat},
		AttrAutoscaleDirection: {AttrLegacyAutoscaleDirection},
		AttrAutoscaleReason:    {AttrLegacyAutoscaleReason},
		AttrUsageInputTokens:   {AttrLegacyUsageInput},
		AttrUsageOutputTokens:  {AttrLegacyUsageOutput},
		AttrUsageCacheRead:     {AttrLegacyUsageCacheRead},
		AttrUsageCacheCreation: {AttrLegacyUsageCacheCreation},
	}
	if len(LegacyAliases) != len(want) {
		t.Fatalf("alias map has %d keys, want %d", len(LegacyAliases), len(want))
	}
	for key, olds := range want {
		got := LegacyAliases[key]
		if len(got) != len(olds) {
			t.Fatalf("%s aliases = %v, want %v", key, got, olds)
		}
		for i := range olds {
			if got[i] != olds[i] {
				t.Errorf("%s alias[%d] = %s, want %s", key, i, got[i], olds[i])
			}
		}
	}
	if _, ok := LegacyAliases[AttrNetworkTransport]; ok {
		t.Fatal("network.transport must not have an alias; only its values change")
	}
	// The cache aliases are the dotted forms emitted today, not underscore forms.
	if string(AttrLegacyUsageCacheRead) != "gen_ai.usage.cache_read.input_tokens" {
		t.Fatalf("cache read alias = %s", AttrLegacyUsageCacheRead)
	}
	if string(AttrLegacyUsageCacheCreation) != "gen_ai.usage.cache_creation.input_tokens" {
		t.Fatalf("cache creation alias = %s", AttrLegacyUsageCacheCreation)
	}
}

func TestSetAttributesWithAliases_StampsLegacy(t *testing.T) {
	exp := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(exp))
	_, span := tp.Tracer("test").Start(t.Context(), "span")
	SetAttributesWithAliases(span,
		attribute.String(string(AttrServerName), "github"),
		attribute.String(string(AttrGenAIToolName), "create_issue"),
		attribute.Int(string(AttrReplicaID), 2),
	)
	span.End()
	recorded := exp.Ended()
	if len(recorded) != 1 {
		t.Fatalf("spans = %d, want 1", len(recorded))
	}
	got := map[string]string{}
	for _, kv := range recorded[0].Attributes() {
		got[string(kv.Key)] = kv.Value.String()
	}
	for _, key := range []string{
		string(AttrServerName), string(AttrLegacyServerName), string(AttrLegacyMCPServerName), string(AttrLegacyMCPServer),
		string(AttrGenAIToolName), string(AttrLegacyToolName), string(AttrLegacyBareToolName),
		string(AttrReplicaID), string(AttrLegacyReplicaID),
	} {
		if got[key] == "" {
			t.Errorf("missing alias %s in %v", key, got)
		}
	}
	if got[string(AttrServerName)] != "github" || got[string(AttrLegacyMCPServer)] != "github" {
		t.Errorf("server aliases = %v", got)
	}
}

func TestAliasRemovalRelease_Documented(t *testing.T) {
	if AliasRemovalRelease != "1.1.0" {
		t.Fatalf("removal release = %q, want 1.1.0", AliasRemovalRelease)
	}
	if !AliasesEnabled() {
		t.Fatal("legacy aliases must stay enabled until " + AliasRemovalRelease)
	}
}

func TestFirstString_PrefersNewKey(t *testing.T) {
	attrs := map[string]string{
		string(AttrLegacyServerName): "old",
		string(AttrServerName):       "new",
	}
	if got := FirstString(attrs, ServerNameKeys()...); got != "new" {
		t.Fatalf("FirstString = %q, want new", got)
	}
	delete(attrs, string(AttrServerName))
	if got := FirstString(attrs, ServerNameKeys()...); got != "old" {
		t.Fatalf("fallback = %q, want old", got)
	}
}

// requirement1Keys are the attribute key literals that may appear only in
// attrs.go among non-test Go files under pkg, internal, and cmd.
func requirement1Keys() []string {
	keys := []string{
		string(AttrMethodName), string(AttrProtocolVersion), string(AttrGenAIToolName),
		string(AttrGenAIOperationName), string(AttrErrorType), string(AttrRPCResponseStatus),
		string(AttrNetworkTransport), string(AttrServerAddress), string(AttrServerPort),
		string(AttrServerName), string(AttrReplicaID), string(AttrClientName),
		string(AttrTransport), string(AttrProtocolGeneration), string(AttrOutputFormat),
		string(AttrAutoscaleDirection), string(AttrAutoscaleReason),
		string(AttrUsageInputTokens), string(AttrUsageOutputTokens),
		string(AttrUsageCacheRead), string(AttrUsageCacheCreation),
		string(AttrLegacyServerName), string(AttrLegacyMCPServerName), string(AttrLegacyMCPServer),
		string(AttrLegacyReplicaID), string(AttrLegacyClientName), string(AttrLegacyProtocolGeneration),
		string(AttrLegacyToolName), string(AttrLegacyBareToolName), string(AttrLegacyOutputFormat),
		string(AttrLegacyAutoscaleDirection), string(AttrLegacyAutoscaleReason),
		string(AttrLegacyUsageInput), string(AttrLegacyUsageOutput),
		string(AttrLegacyUsageCacheRead), string(AttrLegacyUsageCacheCreation),
	}
	return keys
}

func TestAttributeKeyLiterals_OnlyInAttrs(t *testing.T) {
	root := moduleRoot(t)
	keys := requirement1Keys()
	var offenders []string
	for _, dir := range []string{"pkg", "internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if strings.HasSuffix(path, filepath.Join("pkg", "tracing", "attrs.go")) {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			src := stripGoComments(string(body))
			rel, _ := filepath.Rel(root, path)
			for _, key := range keys {
				if strings.Contains(src, `"`+key+`"`) {
					offenders = append(offenders, rel+": "+key)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("attribute key literals outside attrs.go:\n%s", strings.Join(offenders, "\n"))
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

var (
	lineComment  = regexp.MustCompile(`//.*`)
	blockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

func stripGoComments(src string) string {
	src = blockComment.ReplaceAllString(src, "")
	return lineComment.ReplaceAllString(src, "")
}
