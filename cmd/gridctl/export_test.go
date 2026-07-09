package main

import "testing"

func TestSanitizeEnvMap(t *testing.T) {
	tests := []struct {
		name string
		key  string
		val  string
		want string
	}{
		{name: "sensitive key gets canonical placeholder", key: "API_TOKEN", val: "raw-secret", want: "${var:MYSRV_API_TOKEN}"},
		{name: "existing var reference untouched", key: "API_TOKEN", val: "${var:CUSTOM}", want: "${var:CUSTOM}"},
		{name: "legacy vault reference untouched", key: "API_TOKEN", val: "${vault:CUSTOM}", want: "${vault:CUSTOM}"},
		{name: "non-sensitive key untouched", key: "LOG_LEVEL", val: "debug", want: "debug"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{tt.key: tt.val}
			sanitizeEnvMap(env, "MYSRV")
			if got := env[tt.key]; got != tt.want {
				t.Errorf("sanitizeEnvMap(%q=%q) = %q, want %q", tt.key, tt.val, got, tt.want)
			}
		})
	}
}
