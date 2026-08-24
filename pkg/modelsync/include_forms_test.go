package modelsync

import (
	"testing"
)

// Regression (B2): a scalar include of exactly our ref must round-trip
// through remove, dropping the key line.
func TestRemoveInclude_ScalarOfOurRef(t *testing.T) {
	parent := "include: gridctl-models.yaml\nmodel_list: []\n"
	if !hasIncludeLine(parent, "gridctl-models.yaml") {
		t.Fatal("scalar ref not recognized")
	}
	out, err := removeIncludeLine(parent, "gridctl-models.yaml", includeAppended, "")
	if err != nil {
		t.Fatal(err)
	}
	if out != "model_list: []\n" {
		t.Errorf("scalar remove: got %q", out)
	}
	// Quoted scalar form too.
	parent = `include: "gridctl-models.yaml"` + "\nmodel_list: []\n"
	if !hasIncludeLine(parent, "gridctl-models.yaml") {
		t.Fatal("quoted scalar ref not recognized")
	}
	out, err = removeIncludeLine(parent, "gridctl-models.yaml", includeAppended, "")
	if err != nil {
		t.Fatal(err)
	}
	if out != "model_list: []\n" {
		t.Errorf("quoted scalar remove: got %q", out)
	}
}

// Regression (B3): quoted and commented forms of our ref must be
// recognized, so upsert stays idempotent instead of duplicating the
// include.
func TestHasIncludeLine_NormalizedForms(t *testing.T) {
	cases := []string{
		"include:\n  - \"gridctl-models.yaml\"\n",
		"include:\n  - 'gridctl-models.yaml'\n",
		"include:\n  - gridctl-models.yaml  # managed by gridctl\n",
		"include:\n  - \"gridctl-models.yaml\"  # managed\n",
		"include: \"gridctl-models.yaml\"\n",
	}
	for _, content := range cases {
		if !hasIncludeLine(content, "gridctl-models.yaml") {
			t.Errorf("ref not recognized in %q", content)
		}
		edit, err := upsertIncludeLine(content, "gridctl-models.yaml")
		if err != nil {
			t.Fatalf("%q: %v", content, err)
		}
		if edit.Mode != "" {
			t.Errorf("upsert must be a no-op for %q, got mode %q content %q", content, edit.Mode, edit.Content)
		}
	}
}

// Regression (B3): removing our ref from a list must match its quoted
// or commented form.
func TestRemoveInclude_NormalizedListItem(t *testing.T) {
	parent := "include:\n  - base.yaml\n  - \"gridctl-models.yaml\"  # managed\n"
	out, err := removeIncludeLine(parent, "gridctl-models.yaml", includeAppended, "")
	if err != nil {
		t.Fatal(err)
	}
	if out != "include:\n  - base.yaml\n" {
		t.Errorf("got %q", out)
	}
}
