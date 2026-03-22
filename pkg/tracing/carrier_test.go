package tracing

import (
	"testing"
)

func TestMetaCarrierGetSet(t *testing.T) {
	c := NewMetaCarrier(nil)
	c.Set("traceparent", "00-abc123-def456-01")
	if got := c.Get("traceparent"); got != "00-abc123-def456-01" {
		t.Errorf("Get = %q, want %q", got, "00-abc123-def456-01")
	}
}

func TestMetaCarrierMissingKey(t *testing.T) {
	c := NewMetaCarrier(nil)
	if got := c.Get("traceparent"); got != "" {
		t.Errorf("Get on missing key = %q, want empty string", got)
	}
}

func TestMetaCarrierNonStringValue(t *testing.T) {
	meta := map[string]any{"traceparent": 42} // non-string value
	c := NewMetaCarrier(meta)
	if got := c.Get("traceparent"); got != "" {
		t.Errorf("Get non-string = %q, want empty string", got)
	}
}

func TestMetaCarrierKeys(t *testing.T) {
	c := NewMetaCarrier(nil)
	c.Set("traceparent", "v1")
	c.Set("tracestate", "v2")
	keys := c.Keys()
	if len(keys) != 2 {
		t.Errorf("Keys() length = %d, want 2", len(keys))
	}
}

func TestMetaCarrierExistingMap(t *testing.T) {
	existing := map[string]any{"other": "value"}
	c := NewMetaCarrier(existing)
	c.Set("traceparent", "tp")
	m := c.Map()
	if m["other"] != "value" {
		t.Error("existing key lost after Set")
	}
	if m["traceparent"] != "tp" {
		t.Error("new key not in Map()")
	}
}
