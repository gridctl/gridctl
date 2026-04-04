package main

import (
	"strings"
	"testing"
)

func TestCountParseableCriteria(t *testing.T) {
	tests := []struct {
		name     string
		criteria []string
		want     int
	}{
		{
			name:     "all valid",
			criteria: []string{"GIVEN a context WHEN the skill is called THEN is not empty", "GIVEN no inputs WHEN the skill is called THEN is not empty"},
			want:     2,
		},
		{
			name:     "all malformed",
			criteria: []string{"GIVN a context WHEN called THEN ok", "the skill is fast", "GIVEN context WHEN called THEN:"},
			want:     0,
		},
		{
			name:     "mixed — some valid",
			criteria: []string{"GIVN a context WHEN called THEN ok", "GIVEN a context WHEN the skill is called THEN is not empty"},
			want:     1,
		},
		{
			name:     "empty list",
			criteria: []string{},
			want:     0,
		},
		{
			name:     "case-insensitive match",
			criteria: []string{"given a context when the skill is called then is not empty"},
			want:     1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := countParseableCriteria(tc.criteria)
			if got != tc.want {
				t.Errorf("countParseableCriteria(%v) = %d, want %d", tc.criteria, got, tc.want)
			}
		})
	}
}

func TestPrintMalformedCriteriaError(t *testing.T) {
	criteria := []string{
		"GIVN a context WHEN the skill is called THEN is not empty",
		"the skill is fast",
		"GIVEN context WHEN called THEN:",
	}

	var sb strings.Builder
	printMalformedCriteriaError(&sb, "my-skill", criteria)
	out := sb.String()

	if !strings.Contains(out, `cannot activate "my-skill"`) {
		t.Error("output missing skill name in header")
	}
	if !strings.Contains(out, "0 of 3 criteria") {
		t.Error("output missing criteria count")
	}
	if !strings.Contains(out, "[1]") || !strings.Contains(out, "[2]") || !strings.Contains(out, "[3]") {
		t.Error("output missing criterion indices")
	}
	if !strings.Contains(out, "GIVN a context WHEN the skill is called THEN is not empty") {
		t.Error("output missing first criterion text")
	}
	if !strings.Contains(out, "does not match GIVEN ... WHEN ... THEN") {
		t.Error("output missing parse failure reason")
	}
	if !strings.Contains(out, "gridctl activate my-skill") {
		t.Error("output missing retry command")
	}
}
