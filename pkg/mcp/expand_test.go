package mcp

import (
	"os"
	"testing"
)

func TestExpandEnvVars(t *testing.T) {
	tests := []struct {
		name  string
		input string
		env   map[string]string
		want  string
	}{
		// Simple ${VAR} expansion
		{
			name:  "simple variable",
			input: `{"url": "${API_URL}"}`,
			env:   map[string]string{"API_URL": "https://api.example.com"},
			want:  `{"url": "https://api.example.com"}`,
		},
		{
			name:  "undefined variable expands to empty",
			input: `{"url": "${UNDEFINED_VAR}"}`,
			env:   nil,
			want:  `{"url": ""}`,
		},
		{
			name:  "empty variable expands to empty",
			input: `{"url": "${EMPTY_VAR}"}`,
			env:   map[string]string{"EMPTY_VAR": ""},
			want:  `{"url": ""}`,
		},

		// ${VAR:-default} expansion
		{
			name:  "default when undefined",
			input: `{"url": "${API_URL:-https://default.example.com}"}`,
			env:   nil,
			want:  `{"url": "https://default.example.com"}`,
		},
		{
			name:  "default when empty",
			input: `{"url": "${API_URL:-https://default.example.com}"}`,
			env:   map[string]string{"API_URL": ""},
			want:  `{"url": "https://default.example.com"}`,
		},
		{
			name:  "no default when defined and non-empty",
			input: `{"url": "${API_URL:-https://default.example.com}"}`,
			env:   map[string]string{"API_URL": "https://real.example.com"},
			want:  `{"url": "https://real.example.com"}`,
		},

		// ${VAR:+replacement} expansion
		{
			name:  "replacement when defined and non-empty",
			input: `{"auth": "${TOKEN:+present}"}`,
			env:   map[string]string{"TOKEN": "secret123"},
			want:  `{"auth": "present"}`,
		},
		{
			name:  "no replacement when undefined",
			input: `{"auth": "${TOKEN:+present}"}`,
			env:   nil,
			want:  `{"auth": ""}`,
		},
		{
			name:  "no replacement when empty",
			input: `{"auth": "${TOKEN:+present}"}`,
			env:   map[string]string{"TOKEN": ""},
			want:  `{"auth": ""}`,
		},

		// Multiple variables
		{
			name:  "multiple variables in one string",
			input: `{"host": "${HOST}", "port": "${PORT:-8080}"}`,
			env:   map[string]string{"HOST": "localhost"},
			want:  `{"host": "localhost", "port": "8080"}`,
		},
		{
			name:  "mixed syntax",
			input: `${SCHEME:-https}://${HOST}:${PORT:-443}`,
			env:   map[string]string{"HOST": "api.example.com"},
			want:  `https://api.example.com:443`,
		},

		// No expansion needed
		{
			name:  "no variables",
			input: `{"url": "https://example.com"}`,
			env:   nil,
			want:  `{"url": "https://example.com"}`,
		},
		{
			name:  "dollar without brace not expanded",
			input: `$VAR is not expanded`,
			env:   map[string]string{"VAR": "value"},
			want:  `$VAR is not expanded`,
		},

		// Real-world OpenAPI examples
		{
			name: "openapi server URL with default",
			input: `{
  "openapi": "3.0.3",
  "info": {"title": "Test", "version": "1.0"},
  "servers": [{"url": "${API_BASE_URL:-http://localhost:8080}"}]
}`,
			env: nil,
			want: `{
  "openapi": "3.0.3",
  "info": {"title": "Test", "version": "1.0"},
  "servers": [{"url": "http://localhost:8080"}]
}`,
		},
		{
			name: "openapi server URL with env var",
			input: `{
  "servers": [{"url": "${API_BASE_URL:-http://localhost:8080}"}]
}`,
			env: map[string]string{"API_BASE_URL": "https://prod.example.com"},
			want: `{
  "servers": [{"url": "https://prod.example.com"}]
}`,
		},

		// Edge cases
		{
			name:  "variable name with underscore",
			input: `${MY_VAR_123}`,
			env:   map[string]string{"MY_VAR_123": "value"},
			want:  `value`,
		},
		{
			name:  "variable starting with underscore",
			input: `${_PRIVATE}`,
			env:   map[string]string{"_PRIVATE": "secret"},
			want:  `secret`,
		},
		{
			name:  "empty default value",
			input: `${VAR:-}`,
			env:   nil,
			want:  ``,
		},
		{
			name:  "default with special chars",
			input: `${URL:-http://localhost:8080/api?key=value}`,
			env:   nil,
			want:  `http://localhost:8080/api?key=value`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Clear test env vars
			os.Unsetenv("API_URL")
			os.Unsetenv("UNDEFINED_VAR")
			os.Unsetenv("EMPTY_VAR")
			os.Unsetenv("API_BASE_URL")
			os.Unsetenv("HOST")
			os.Unsetenv("PORT")
			os.Unsetenv("SCHEME")
			os.Unsetenv("TOKEN")
			os.Unsetenv("VAR")
			os.Unsetenv("MY_VAR_123")
			os.Unsetenv("_PRIVATE")
			os.Unsetenv("URL")

			// Set test env vars
			for k, v := range tc.env {
				os.Setenv(k, v)
			}

			got := string(expandEnvVars([]byte(tc.input)))
			if got != tc.want {
				t.Errorf("expandEnvVars() =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}
