package search

import "testing"

func TestFuseRRFCombinesStreamsAndDiversifiesSessions(t *testing.T) {
	bm25 := []Ranked{{ID: 1, SessionID: "s1"}, {ID: 2, SessionID: "s1"}, {ID: 3, SessionID: "s2"}}
	vector := []Ranked{{ID: 3, SessionID: "s2"}, {ID: 2, SessionID: "s1"}, {ID: 4, SessionID: "s1"}}

	got := FuseRRF(bm25, vector, 60, 3, 1)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}
	if got[0].ID != 3 {
		t.Fatalf("first id = %d, want vector+bm25 winner 3: %+v", got[0].ID, got)
	}
	if got[1].SessionID == "s1" && got[0].SessionID == "s1" {
		t.Fatalf("session diversification failed: %+v", got)
	}
}
