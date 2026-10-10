package deals

import "testing"

func panelFixture() ([]string, []string) {
	bonded := []string{"creator", "beneficiary", "node", "v1", "v2", "v3", "v4", "v5", "v6"}
	exclude := []string{"creator", "beneficiary", "node"}
	return bonded, exclude
}

func TestOpenStoresTermsOnly(t *testing.T) {
	b := NewBook()
	bonded, exclude := panelFixture()
	brief, err := b.Open("ship the goods", "100", "escrow-1", bonded, exclude)
	if err != nil {
		t.Fatal(err)
	}
	if brief.CaseID == "escrow-1" || brief.CaseID == "" {
		t.Fatalf("case id %q", brief.CaseID)
	}
	if brief.Terms != "ship the goods" || brief.TermsHash == "" {
		t.Fatalf("%+v", brief)
	}
	again, err := b.Open("ship the goods", "100", "escrow-1", bonded, exclude)
	if err != nil {
		t.Fatal(err)
	}
	if again.TermsHash != brief.TermsHash {
		t.Fatal("hash must follow the terms text")
	}
}

func TestPickFiveStableAndExcludesParties(t *testing.T) {
	bonded, exclude := panelFixture()
	a, err := PickFive("9||escrow-1", bonded, exclude)
	if err != nil {
		t.Fatal(err)
	}
	b, err := PickFive("9||escrow-1", bonded, exclude)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 5 {
		t.Fatalf("%v", a)
	}
	joined := map[string]bool{}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("unstable %v vs %v", a, b)
		}
		joined[a[i]] = true
	}
	for _, banned := range exclude {
		if joined[banned] {
			t.Fatalf("picked %s", banned)
		}
	}
}

func TestOpenFailsClosedWhenPanelIsShort(t *testing.T) {
	b := NewBook()
	_, err := b.Open("terms", "1", "escrow-9", []string{"a", "b", "creator"}, []string{"creator"})
	if err == nil {
		t.Fatal("expected short panel to fail")
	}
	if b.Count() != 0 {
		t.Fatal("failed open must not store a case")
	}
}

func TestCommitAndReveal(t *testing.T) {
	b := NewBook()
	bonded, exclude := panelFixture()
	brief, err := b.Open("terms", "1", "escrow-2", bonded, exclude)
	if err != nil {
		t.Fatal(err)
	}
	five, _ := PickFive("1||escrow-2", bonded, exclude)
	if err := b.Commit(brief.CaseID, "outsider", HashVerdict(VerdictFulfilled)); err == nil {
		t.Fatal("outsider commit")
	}
	if err := b.Commit(brief.CaseID, five[0], HashVerdict(VerdictFulfilled)); err != nil {
		t.Fatal(err)
	}
	if err := b.Commit(brief.CaseID, five[0], HashVerdict(VerdictFulfilled)); err == nil {
		t.Fatal("second commit")
	}
	if err := b.Reveal(brief.CaseID, five[0], "maybe"); err == nil {
		t.Fatal("bad verdict")
	}
	if err := b.Reveal(brief.CaseID, five[0], VerdictNotFulfilled); err == nil {
		t.Fatal("mismatched reveal")
	}
	if err := b.Reveal(brief.CaseID, five[0], VerdictFulfilled); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 3; i++ {
		if err := b.Commit(brief.CaseID, five[i], HashVerdict(VerdictFulfilled)); err != nil {
			t.Fatal(err)
		}
		if err := b.Reveal(brief.CaseID, five[i], VerdictFulfilled); err != nil {
			t.Fatal(err)
		}
	}
	q, ok := b.Quorum(brief.CaseID)
	if !ok || q != VerdictFulfilled {
		t.Fatalf("quorum %q %v", q, ok)
	}
	if err := b.Commit(brief.CaseID, five[3], HashVerdict(VerdictNotFulfilled)); err != nil {
		t.Fatal(err)
	}
	if err := b.Reveal(brief.CaseID, five[3], VerdictNotFulfilled); err != nil {
		t.Fatal(err)
	}
	q, _ = b.Quorum(brief.CaseID)
	if q != VerdictFulfilled {
		t.Fatalf("quorum flipped to %q", q)
	}
}
