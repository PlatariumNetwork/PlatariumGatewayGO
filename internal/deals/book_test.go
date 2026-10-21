package deals

import (
	"os"
	"path/filepath"
	"testing"
)

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
	again, err := b.Open("other text", "999", "escrow-1", bonded, exclude)
	if err != nil {
		t.Fatal(err)
	}
	if again.CaseID != brief.CaseID {
		t.Fatal("repeat open must keep the same case")
	}
}

func TestPickFiveStableAndExcludesParties(t *testing.T) {
	bonded, exclude := panelFixture()
	seed := string(PanelSeed("escrow-1", "terms", PanelInstanceFirst, PanelGenerationFirst))
	a, err := PickFive(seed, bonded, exclude)
	if err != nil {
		t.Fatal(err)
	}
	b, err := PickFive(seed, bonded, exclude)
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

func TestPanelSeedIs89BytesAndIgnoresOpenBlock(t *testing.T) {
	s := PanelSeed("escrow-1", "terms", PanelInstanceFirst, PanelGenerationFirst)
	if len(s) != 89 {
		t.Fatalf("len %d", len(s))
	}
	if string(s[:23]) != PanelDomain {
		t.Fatalf("domain %q", s[:23])
	}
	b := NewBook()
	bonded, exclude := panelFixture()
	first, err := b.Open("terms", "1", "escrow-x", bonded, exclude)
	if err != nil {
		t.Fatal(err)
	}
	p1, _ := b.Panel(first.CaseID)
	b2 := NewBook()
	second, err := b2.Open("terms", "5000", "escrow-x", bonded, exclude)
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := b2.Panel(second.CaseID)
	if len(p1) != 5 {
		t.Fatal(p1)
	}
	for i := range p1 {
		if p1[i] != p2[i] {
			t.Fatalf("requested block changed seats %v %v", p1, p2)
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

func TestRosterUsedInsteadOfRequestBonded(t *testing.T) {
	b := NewBook()
	b.SetRoster([]string{"creator", "beneficiary", "node", "r1", "r2", "r3", "r4", "r5"})
	brief, err := b.Open("terms", "1", "escrow-r", []string{"v1", "v2", "v3", "v4", "v5", "v6", "creator"}, []string{"creator", "beneficiary", "node"})
	if err != nil {
		t.Fatal(err)
	}
	panel, _ := b.Panel(brief.CaseID)
	for _, p := range panel {
		if p == "v1" {
			t.Fatalf("used request bonded %v", panel)
		}
	}
}

func TestCommitAndReveal(t *testing.T) {
	b := NewBook()
	bonded, exclude := panelFixture()
	brief, err := b.Open("terms", "1", "escrow-2", bonded, exclude)
	if err != nil {
		t.Fatal(err)
	}
	five, ok := b.Panel(brief.CaseID)
	if !ok {
		t.Fatal("panel")
	}
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
	hidden, _ := b.Reveals(brief.CaseID)
	if len(hidden) != 0 {
		t.Fatalf("reveals leaked before freeze %v", hidden)
	}
	if _, ok := b.Quorum(brief.CaseID); ok {
		t.Fatal("quorum before freeze")
	}
	for i := 1; i < 3; i++ {
		if err := b.Commit(brief.CaseID, five[i], HashVerdict(VerdictFulfilled)); err != nil {
			t.Fatal(err)
		}
		if err := b.Reveal(brief.CaseID, five[i], VerdictFulfilled); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.TimeoutAllowed(brief.CaseID); err == nil {
		t.Fatal("timeout before quorum")
	}
	b.SetHeight(2)
	q, ok := b.Quorum(brief.CaseID)
	if !ok || q != OutcomeFulfilled {
		t.Fatalf("quorum %q %v", q, ok)
	}
	shown, _ := b.Reveals(brief.CaseID)
	if len(shown) < 3 {
		t.Fatalf("expected reveals after freeze %v", shown)
	}
	if err := b.Commit(brief.CaseID, five[3], HashVerdict(VerdictNotFulfilled)); err != nil {
		t.Fatal(err)
	}
	if err := b.Reveal(brief.CaseID, five[3], VerdictNotFulfilled); err != nil {
		t.Fatal(err)
	}
	q, _ = b.Quorum(brief.CaseID)
	if q != OutcomeFulfilled {
		t.Fatalf("quorum flipped to %q", q)
	}
}

func TestNonceCommit(t *testing.T) {
	b := NewBook()
	bonded, exclude := panelFixture()
	brief, err := b.Open("terms", "1", "escrow-n", bonded, exclude)
	if err != nil {
		t.Fatal(err)
	}
	five, _ := b.Panel(brief.CaseID)
	if err := b.Commit(brief.CaseID, five[0], HashCommit(VerdictFulfilled, "secret")); err != nil {
		t.Fatal(err)
	}
	if err := b.RevealNonce(brief.CaseID, five[0], VerdictFulfilled, "nope"); err == nil {
		t.Fatal("bad nonce")
	}
	if err := b.RevealNonce(brief.CaseID, five[0], VerdictFulfilled, "secret"); err != nil {
		t.Fatal(err)
	}
}

func TestPersistSurvivesRestartAndTornWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "volunteers.json")
	b := OpenBook(path)
	bonded, exclude := panelFixture()
	brief, err := b.Open("terms", "3", "escrow-p", bonded, exclude)
	if err != nil {
		t.Fatal(err)
	}
	b.SetRoster(bonded)
	again := OpenBook(path)
	if !again.Has(brief.CaseID) {
		t.Fatal("case missing after reopen")
	}
	if len(again.Roster()) != len(bonded) {
		t.Fatalf("roster %v", again.Roster())
	}
	tmp := filepath.Join(dir, "volunteers-torn.tmp")
	if err := os.WriteFile(tmp, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".junk", []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	third := OpenBook(path)
	if !third.Has(brief.CaseID) {
		t.Fatal("complete file must win over a torn temp")
	}
}

func TestAppealBriefCarriesPriorResult(t *testing.T) {
	b := NewBook()
	bonded, exclude := panelFixture()
	brief, err := b.OpenAppeal("terms", "1", "escrow-a", bonded, exclude, OutcomeFulfilled)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := b.Brief(brief.CaseID)
	if got.PriorResult != OutcomeFulfilled {
		t.Fatalf("%+v", got)
	}
}
