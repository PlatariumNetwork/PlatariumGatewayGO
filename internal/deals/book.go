// Package deals stores volunteer dispute briefs on Gateway.
// The brief a volunteer can read is terms and a hash. Parties, amount, and the escrow id stay off that record.
package deals

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
)

const (
	VerdictFulfilled    = "fulfilled"
	VerdictNotFulfilled = "not-fulfilled"
)

// Brief is the only record served to a volunteer.
type Brief struct {
	CaseID     string `json:"caseId"`
	Terms      string `json:"terms"`
	TermsHash  string `json:"termsHash"`
}

type cse struct {
	brief    Brief
	escrowID string
	panel    map[string]struct{}
	commits  map[string]string
	reveals  map[string]string
	quorum   string
}

// Book is the in-memory volunteer panel for this gateway process.
type Book struct {
	mu    sync.Mutex
	cases map[string]*cse
}

func NewBook() *Book {
	return &Book{cases: map[string]*cse{}}
}

// Open stores a terms-only case and picks five volunteers.
// Fewer than five eligible volunteers returns an error and stores nothing.
func (b *Book) Open(terms, openBlock, escrowID string, bonded []string, exclude []string) (Brief, error) {
	terms = stringsTrim(terms)
	if terms == "" || escrowID == "" {
		return Brief{}, fmt.Errorf("terms and escrow id required")
	}
	panel, err := PickFive(openBlock+"||"+escrowID, bonded, exclude)
	if err != nil {
		return Brief{}, err
	}
	sum := sha256.Sum256([]byte(terms))
	hash := hex.EncodeToString(sum[:])
	idSum := sha256.Sum256([]byte("deal-case|" + escrowID + "|" + hash))
	caseID := hex.EncodeToString(idSum[:])
	if caseID == escrowID {
		return Brief{}, fmt.Errorf("case id collided with escrow id")
	}
	brief := Brief{CaseID: caseID, Terms: terms, TermsHash: hash}
	set := make(map[string]struct{}, len(panel))
	for _, p := range panel {
		set[p] = struct{}{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cases[caseID] = &cse{
		brief:    brief,
		escrowID: escrowID,
		panel:    set,
		commits:  map[string]string{},
		reveals:  map[string]string{},
	}
	return brief, nil
}

func (b *Book) Brief(caseID string) (Brief, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.cases[caseID]
	if !ok {
		return Brief{}, false
	}
	return c.brief, true
}

func (b *Book) Commit(caseID, key, verdictHash string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.cases[caseID]
	if !ok {
		return fmt.Errorf("unknown case")
	}
	if _, on := c.panel[key]; !on {
		return fmt.Errorf("not on the panel")
	}
	if _, exists := c.commits[key]; exists {
		return fmt.Errorf("already committed")
	}
	if verdictHash == "" {
		return fmt.Errorf("verdict hash required")
	}
	c.commits[key] = verdictHash
	return nil
}

func (b *Book) Reveal(caseID, key, verdict string) error {
	if verdict != VerdictFulfilled && verdict != VerdictNotFulfilled {
		return fmt.Errorf("verdict must be fulfilled or not-fulfilled")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.cases[caseID]
	if !ok {
		return fmt.Errorf("unknown case")
	}
	if _, on := c.panel[key]; !on {
		return fmt.Errorf("not on the panel")
	}
	want, committed := c.commits[key]
	if !committed || want != hashVerdict(verdict) {
		return fmt.Errorf("reveal does not match commit")
	}
	c.reveals[key] = verdict
	if c.quorum == "" {
		if outcome, ok := quorumOf(c.reveals); ok {
			c.quorum = outcome
		}
	}
	return nil
}

// Quorum is the first outcome that three reveals agreed on. A later opposite reveal does not replace it.
func (b *Book) Quorum(caseID string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.cases[caseID]
	if !ok || c.quorum == "" {
		return "", false
	}
	return c.quorum, true
}

func (b *Book) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.cases)
}

func (b *Book) Has(caseID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.cases[caseID]
	return ok
}

func hashVerdict(verdict string) string {
	sum := sha256.Sum256([]byte(verdict))
	return hex.EncodeToString(sum[:])
}

// HashVerdict is the commit value for a verdict string.
func HashVerdict(verdict string) string { return hashVerdict(verdict) }

func quorumOf(reveals map[string]string) (string, bool) {
	counts := map[string]int{}
	for _, v := range reveals {
		counts[v]++
		if counts[v] >= 3 {
			return v, true
		}
	}
	return "", false
}

// PickFive orders the bonded set by hash(seed || address) after dropping exclude, then takes five.
func PickFive(seed string, bonded, exclude []string) ([]string, error) {
	drop := map[string]struct{}{}
	for _, e := range exclude {
		if e != "" {
			drop[e] = struct{}{}
		}
	}
	eligible := make([]string, 0, len(bonded))
	seen := map[string]struct{}{}
	for _, addr := range bonded {
		if addr == "" {
			continue
		}
		if _, bad := drop[addr]; bad {
			continue
		}
		if _, dup := seen[addr]; dup {
			continue
		}
		seen[addr] = struct{}{}
		eligible = append(eligible, addr)
	}
	if len(eligible) < 5 {
		return nil, fmt.Errorf("fewer than five eligible volunteers")
	}
	sort.Slice(eligible, func(i, j int) bool {
		hi := sha256.Sum256([]byte(seed + "|" + eligible[i]))
		hj := sha256.Sum256([]byte(seed + "|" + eligible[j]))
		cmp := 0
		for k := 0; k < len(hi); k++ {
			if hi[k] != hj[k] {
				if hi[k] < hj[k] {
					cmp = -1
				} else {
					cmp = 1
				}
				break
			}
		}
		if cmp == 0 {
			return eligible[i] < eligible[j]
		}
		return cmp < 0
	})
	return append([]string(nil), eligible[:5]...), nil
}

func stringsTrim(s string) string {
	i := 0
	j := len(s)
	for i < j && (s[i] == ' ' || s[i] == '\n' || s[i] == '\t') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\n' || s[j-1] == '\t') {
		j--
	}
	return s[i:j]
}
