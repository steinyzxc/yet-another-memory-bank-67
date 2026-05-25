package bench

import "testing"

func TestMetricsAtK(t *testing.T) {
	ranked := []string{"m1", "m2", "m3", "m4"}
	gold := []string{"m2", "m4", "m9"}
	if got := PrecisionAt(ranked, gold, 3); got != 1.0/3.0 {
		t.Fatalf("precision@3 = %v", got)
	}
	if got := RecallAt(ranked, gold, 3); got != 1.0/3.0 {
		t.Fatalf("recall@3 = %v", got)
	}
	if got := HitAt(ranked, gold, 3); got != 1 {
		t.Fatalf("hit@3 = %v", got)
	}
	if got := MRR(ranked, gold); got != 0.5 {
		t.Fatalf("mrr = %v", got)
	}
	if got := NDCGAt(ranked, gold, 3); got < 0.29 || got > 0.30 {
		t.Fatalf("ndcg@3 = %v", got)
	}
}

func TestMetricsHandleEmptyGold(t *testing.T) {
	if PrecisionAt([]string{"m1"}, nil, 5) != 0 || RecallAt([]string{"m1"}, nil, 5) != 0 || HitAt([]string{"m1"}, nil, 5) != 0 || MRR([]string{"m1"}, nil) != 0 || NDCGAt([]string{"m1"}, nil, 5) != 0 {
		t.Fatal("empty gold metrics should be zero")
	}
}
