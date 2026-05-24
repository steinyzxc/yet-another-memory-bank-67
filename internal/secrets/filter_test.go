package secrets

import (
	"strings"
	"testing"
)

func TestRedactReplacesKnownSecrets(t *testing.T) {
	input := `{"aws":"AKIA1234567890ABCDEF","jwt":"eyJabcdefghi.eyJklmnopqr.signaturexyz","gh":"ghp_abcdefghijklmnopqrstuvwxyzABCDEFGHIJ"}`
	out := Redact(input)
	if strings.Contains(out, "AKIA1234567890ABCDEF") || strings.Contains(out, "ghp_") || strings.Contains(out, "eyJabcdefghi") {
		t.Fatalf("secret leaked: %s", out)
	}
	if got := strings.Count(out, "[REDACTED]"); got != 3 {
		t.Fatalf("redaction count = %d, want 3 in %s", got, out)
	}
}
