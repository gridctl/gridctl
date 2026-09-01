package builder

import (
	"strings"
	"testing"
)

func TestSelectPythonVersion_Specifiers(t *testing.T) {
	tests := []struct {
		spec     string
		explicit string
		want     string
	}{
		{">=3.11,<3.13", "", "3.11"},
		{"~=3.12.1", "", "3.12"},
		{"!=3.10.*,>=3.10", "", "3.11"},
		{"(>=3.13rc1)", "", "3.13"},
		{"<3.12.post1", "", "3.10"},
		{"", "", "3.12"},
		{">=3.10", "3.13", "3.13"},
	}
	for _, test := range tests {
		got, err := SelectPythonVersion(test.spec, test.explicit)
		if err != nil {
			t.Errorf("SelectPythonVersion(%q, %q): %v", test.spec, test.explicit, err)
			continue
		}
		if got != test.want {
			t.Errorf("SelectPythonVersion(%q, %q) = %q, want %q", test.spec, test.explicit, got, test.want)
		}
	}
}

func TestSelectPythonVersion_IncompatibleErrorContract(t *testing.T) {
	_, err := SelectPythonVersion(">=3.14", "")
	if err == nil || !strings.Contains(err.Error(), "Package requires Python >=3.14, which is incompatible with image selection") {
		t.Fatalf("error = %v", err)
	}
}
