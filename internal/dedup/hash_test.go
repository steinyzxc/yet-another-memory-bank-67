package dedup

import "testing"

func TestHashCanonicalJSONIgnoresObjectKeyOrder(t *testing.T) {
	a := []byte(`{"tool_input":{"b":2,"a":1},"tool_response":{"ok":true}}`)
	b := []byte(`{"tool_response":{"ok":true},"tool_input":{"a":1,"b":2}}`)

	ha, err := HashCanonicalJSON(a)
	if err != nil {
		t.Fatalf("hash a: %v", err)
	}
	hb, err := HashCanonicalJSON(b)
	if err != nil {
		t.Fatalf("hash b: %v", err)
	}
	if ha != hb {
		t.Fatalf("hash mismatch: %s != %s", ha, hb)
	}
}

func TestHashCanonicalJSONRejectsInvalidJSON(t *testing.T) {
	_, err := HashCanonicalJSON([]byte(`{"broken"`))
	if err == nil {
		t.Fatal("expected invalid JSON error")
	}
}
