package registry

import "testing"

// TestSaveSkill_CachesCanonicalForm: SaveSkill must cache the parse of what
// it wrote, not the caller's object. A JSON-supplied skill need not be a
// parse/render fixed point (CRLF bodies, empty state), and a cache that
// differs from the next disk load reads as phantom content change to
// anything hashing the canonical form.
func TestSaveSkill_CachesCanonicalForm(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	sk := &AgentSkill{
		Name:        "crlf",
		Description: "d",
		Body:        "line one\r\nline two\r\n", // CRLF: parse normalizes, render preserves
		// State deliberately empty: parse defaults it to draft.
	}
	if err := store.SaveSkill(sk); err != nil {
		t.Fatal(err)
	}

	cached, err := store.GetSkill("crlf")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.GetSkill("crlf")
	if err != nil {
		t.Fatal(err)
	}

	if cached.Body != reloaded.Body {
		t.Fatalf("cached body != disk-loaded body:\n cached: %q\n loaded: %q", cached.Body, reloaded.Body)
	}
	if cached.State != reloaded.State {
		t.Fatalf("cached state %q != disk-loaded state %q", cached.State, reloaded.State)
	}

	// The caller's object is updated to the canonical form too, so API
	// responses echo what was actually stored.
	if sk.Body != cached.Body || sk.State != cached.State {
		t.Fatalf("caller's object not canonicalized: body %q state %q", sk.Body, sk.State)
	}
}
