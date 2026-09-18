package logging

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func diagnosticHandle(t *testing.T, kind string) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return "gca2a_" + kind + "1_" + base64.RawURLEncoding.EncodeToString(b)
}

func TestRedactString_Capabilities(t *testing.T) {
	for _, kind := range []string{"c", "t"} {
		handle := diagnosticHandle(t, kind)
		for _, input := range []string{handle, "prefix" + handle + "suffix", fmt.Sprintf(`{"%s":"%s"}`, handle, handle)} {
			got := RedactString(input)
			if strings.Contains(got, handle) || !strings.Contains(got, "[REDACTED]") {
				t.Fatal("typed capability was not sanitized")
			}
		}
	}
	if got := RedactString("ordinary diagnostic"); got != "ordinary diagnostic" {
		t.Fatal("ordinary diagnostic changed")
	}
}

func TestRedactingHandler_CapabilityFields(t *testing.T) {
	handle := diagnosticHandle(t, "t")
	var buf bytes.Buffer
	logger := slog.New(NewRedactingHandler(slog.NewJSONHandler(&buf, nil)))
	logger.WithGroup(handle).With(handle, handle).Info(handle,
		slog.Group(handle, slog.String(handle, handle)),
		slog.Any("map", map[string]string{handle: handle}),
		slog.Any("nested", map[string]any{handle: []any{map[string]any{"value": handle}}}),
		slog.Any("error", fmt.Errorf("failed %s", handle)),
	)
	if strings.Contains(buf.String(), handle) || !strings.Contains(buf.String(), "[REDACTED]") {
		t.Fatal("capability leaked through slog field or group")
	}
}
