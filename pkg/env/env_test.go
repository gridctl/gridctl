package env

import "testing"

func TestBool(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		set     bool
		want    *bool
		wantErr bool
	}{
		{name: "unset defers", set: false, want: nil},
		{name: "empty defers", value: "", set: true, want: nil},
		{name: "one is true", value: "1", set: true, want: boolPtr(true)},
		{name: "zero is false", value: "0", set: true, want: boolPtr(false)},
		{name: "true lowercase", value: "true", set: true, want: boolPtr(true)},
		{name: "TRUE uppercase", value: "TRUE", set: true, want: boolPtr(true)},
		{name: "False mixed", value: "False", set: true, want: boolPtr(false)},
		{name: "t shorthand", value: "t", set: true, want: boolPtr(true)},
		{name: "yes is an error, not false", value: "yes", set: true, wantErr: true},
		{name: "garbage is an error", value: "banana", set: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const key = "GRIDCTL_TEST_ENV_BOOL"
			if tt.set {
				t.Setenv(key, tt.value)
			}
			got, err := Bool(key)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Bool(%q)=%v, want error", tt.value, got)
				}
				if got != nil {
					t.Fatalf("Bool(%q) returned a value alongside the error", tt.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("Bool(%q) unexpected error: %v", tt.value, err)
			}
			if (got == nil) != (tt.want == nil) {
				t.Fatalf("Bool(%q)=%v, want %v", tt.value, got, tt.want)
			}
			if got != nil && *got != *tt.want {
				t.Fatalf("Bool(%q)=%v, want %v", tt.value, *got, *tt.want)
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }
