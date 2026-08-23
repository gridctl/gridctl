package modelsync

import (
	"strings"
	"testing"
)

const parentWithComments = `# my litellm config
model_list:
  - model_name: qwen-local   # the GPU box
    litellm_params:
      model: openai/qwen3
      api_base: http://127.0.0.1:8000/v1
      api_key: os.environ/DUMMY_KEY

router_settings:
  num_retries: 2   # keep retries modest
`

func TestUpsertInclude_CreatesKey(t *testing.T) {
	edit, err := upsertIncludeLine(parentWithComments, "gridctl-models.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if edit.Mode != includeCreated {
		t.Fatalf("mode = %q, want created", edit.Mode)
	}
	if !strings.HasPrefix(edit.Content, parentWithComments) {
		t.Errorf("everything before the appended key must be byte-identical")
	}
	if !strings.HasSuffix(edit.Content, "include:\n  - gridctl-models.yaml\n") {
		t.Errorf("appended include block malformed:\n%s", edit.Content)
	}
	if !hasIncludeLine(edit.Content, "gridctl-models.yaml") {
		t.Error("hasIncludeLine should see the new entry")
	}

	// Round trip: removal restores the original bytes exactly.
	restored, err := removeIncludeLine(edit.Content, "gridctl-models.yaml", edit.Mode, edit.Original)
	if err != nil {
		t.Fatal(err)
	}
	if restored != parentWithComments {
		t.Errorf("remove did not restore the original:\n%q\nwant\n%q", restored, parentWithComments)
	}
}

func TestUpsertInclude_AppendsToList(t *testing.T) {
	parent := "include:\n  - base.yaml   # keep first\n  - extra.yaml\nmodel_list: []\n"
	edit, err := upsertIncludeLine(parent, "gridctl-models.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if edit.Mode != includeAppended {
		t.Fatalf("mode = %q, want appended", edit.Mode)
	}
	want := "include:\n  - base.yaml   # keep first\n  - extra.yaml\n  - gridctl-models.yaml\nmodel_list: []\n"
	if edit.Content != want {
		t.Errorf("got:\n%q\nwant:\n%q", edit.Content, want)
	}
	restored, err := removeIncludeLine(edit.Content, "gridctl-models.yaml", edit.Mode, "")
	if err != nil {
		t.Fatal(err)
	}
	if restored != parent {
		t.Errorf("remove did not restore:\n%q\nwant\n%q", restored, parent)
	}
}

func TestUpsertInclude_PromotesScalar(t *testing.T) {
	parent := "include: base.yaml # shared models\nmodel_list: []\n"
	edit, err := upsertIncludeLine(parent, "gridctl-models.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if edit.Mode != includePromoted || edit.Original != "base.yaml" {
		t.Fatalf("mode/original = %q/%q, want promoted/base.yaml", edit.Mode, edit.Original)
	}
	want := "include: # shared models\n  - base.yaml\n  - gridctl-models.yaml\nmodel_list: []\n"
	if edit.Content != want {
		t.Errorf("got:\n%q\nwant:\n%q", edit.Content, want)
	}
	restored, err := removeIncludeLine(edit.Content, "gridctl-models.yaml", edit.Mode, edit.Original)
	if err != nil {
		t.Fatal(err)
	}
	if restored != "include: base.yaml # shared models\nmodel_list: []\n" {
		t.Errorf("scalar not restored: %q", restored)
	}
}

func TestUpsertInclude_FlowList(t *testing.T) {
	parent := "include: [base.yaml, extra.yaml]\n"
	edit, err := upsertIncludeLine(parent, "gridctl-models.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if edit.Mode != includeFlow {
		t.Fatalf("mode = %q, want flow", edit.Mode)
	}
	if edit.Content != "include: [base.yaml, extra.yaml, gridctl-models.yaml]\n" {
		t.Errorf("got %q", edit.Content)
	}
	restored, err := removeIncludeLine(edit.Content, "gridctl-models.yaml", edit.Mode, "")
	if err != nil {
		t.Fatal(err)
	}
	if restored != parent {
		t.Errorf("flow remove: got %q want %q", restored, parent)
	}
}

func TestUpsertInclude_Idempotent(t *testing.T) {
	parent := "include:\n  - gridctl-models.yaml\n"
	edit, err := upsertIncludeLine(parent, "gridctl-models.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if edit.Mode != "" || edit.Content != parent {
		t.Errorf("second upsert must be a no-op, got mode %q content %q", edit.Mode, edit.Content)
	}
}

func TestUpsertInclude_PreservesCRLFViaCaller(t *testing.T) {
	parent := "model_list: []\r\n"
	edit, err := upsertIncludeLine(parent, "gridctl-models.yaml")
	if err != nil {
		t.Fatal(err)
	}
	out := restoreCRLF(parent, edit.Content)
	if !strings.Contains(out, "model_list: []\r\n") || !strings.Contains(out, "include:\r\n") {
		t.Errorf("CRLF not preserved: %q", out)
	}
}

func TestRemoveInclude_AbsentIsNoop(t *testing.T) {
	parent := "model_list: []\n"
	out, err := removeIncludeLine(parent, "gridctl-models.yaml", includeCreated, "")
	if err != nil {
		t.Fatal(err)
	}
	if out != parent {
		t.Errorf("got %q", out)
	}
}
