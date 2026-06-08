package rating

import "testing"

func TestApplySlashAgainstMajority(t *testing.T) {
	r := NewRegistry()
	r.EnsureNode("n1", 1000, 1)
	before := r.Get("n1")
	if before == nil {
		t.Fatal("node missing")
	}
	beforeScore := before.ReputationScore
	beforeStake := before.Stake
	if err := r.ApplySlash("n1", SlashAgainstMajority); err != nil {
		t.Fatalf("ApplySlash: %v", err)
	}
	after := r.Get("n1")
	if after.ReputationScore >= beforeScore {
		t.Fatalf("expected reputation drop, before=%d after=%d", beforeScore, after.ReputationScore)
	}
	if after.Stake != beforeStake-stakeSlashFor(SlashAgainstMajority) {
		t.Fatalf("expected stake slash, before=%d after=%d", beforeStake, after.Stake)
	}
}

func TestApplySlashBatchSuspendsAfterRepeatedPenalties(t *testing.T) {
	r := NewRegistry()
	r.EnsureNode("n1", 1000, 1)
	for i := 0; i < 7; i++ {
		r.ApplySlashBatch([]string{"n1"}, SlashEquivocation)
	}
	n := r.Get("n1")
	if n == nil || n.Status != NodeStatusSuspended {
		t.Fatalf("expected suspended status, got %+v", n)
	}
}
