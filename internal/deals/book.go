package deals

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

const (
	VerdictFulfilled    = "fulfilled"
	VerdictNotFulfilled = "not-fulfilled"
	OutcomeFulfilled    = "dispute-fulfilled"
	OutcomeNotFulfilled = "dispute-not-fulfilled"
	PanelDomain         = "platarium-deal-panel-v1"
	PanelInstanceFirst  = byte(0x01)
	PanelGenerationFirst = byte(0x01)
)

// Brief is the only record served to a volunteer.
type Brief struct {
	CaseID       string `json:"caseId"`
	Terms        string `json:"terms"`
	TermsHash    string `json:"termsHash"`
	PriorResult  string `json:"priorResult,omitempty"`
	RevealsOpen  bool   `json:"revealsOpen,omitempty"`
}

type cse struct {
	brief      Brief
	escrowID   string
	panel      []string
	panelSet   map[string]struct{}
	commits    map[string]string
	reveals    map[string]string
	quorum     string
	deadline   uint64
	frozen     bool
	instance   byte
	generation byte
}

// Book is the volunteer panel for this gateway process.
type Book struct {
	mu       sync.Mutex
	cases    map[string]*cse
	byEscrow map[string]string
	roster   []string
	height   uint64
	path     string
}

func NewBook() *Book {
	return &Book{cases: map[string]*cse{}, byEscrow: map[string]string{}}
}

func OpenBook(path string) *Book {
	b := NewBook()
	b.path = path
	if path == "" {
		return b
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return b
	}
	var snap persistSnap
	if json.Unmarshal(raw, &snap) != nil {
		return b
	}
	b.roster = append([]string(nil), snap.Roster...)
	b.height = snap.Height
	for _, row := range snap.Cases {
		c := row.toCase()
		b.cases[c.brief.CaseID] = c
		b.byEscrow[c.escrowID] = c.brief.CaseID
	}
	return b
}

func (b *Book) SetRoster(keys []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	seen := map[string]struct{}{}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	b.roster = out
	b.flushLocked()
}

func (b *Book) Roster() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.roster...)
}

func (b *Book) SetHeight(h uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.height = h
	for _, c := range b.cases {
		if !c.frozen && h > c.deadline {
			c.frozen = true
			if c.quorum == "" {
				if q, ok := quorumOf(c.reveals); ok {
					c.quorum = q
				}
			}
		}
	}
	b.flushLocked()
}

func (b *Book) Height() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.height
}

// Open stores a terms-only case and seats five volunteers from the roster (or bonded fallback).
func (b *Book) Open(terms, openBlock, escrowID string, bonded []string, exclude []string) (Brief, error) {
	return b.OpenAppeal(terms, openBlock, escrowID, bonded, exclude, "")
}

func (b *Book) OpenAppeal(terms, openBlock, escrowID string, bonded []string, exclude []string, prior string) (Brief, error) {
	terms = stringsTrim(terms)
	if terms == "" || escrowID == "" {
		return Brief{}, fmt.Errorf("terms and escrow id required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if id, ok := b.byEscrow[escrowID]; ok {
		c := b.cases[id]
		out := c.brief
		out.PriorResult = prior
		c.brief.PriorResult = prior
		return out, nil
	}
	pool := b.roster
	if len(pool) == 0 {
		pool = bonded
	}
	seed := PanelSeed(escrowID, terms, PanelInstanceFirst, PanelGenerationFirst)
	panel, err := PickFive(string(seed), pool, exclude)
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
	deadline := parseUint(openBlock)
	brief := Brief{CaseID: caseID, Terms: terms, TermsHash: hash, PriorResult: prior}
	set := make(map[string]struct{}, len(panel))
	for _, p := range panel {
		set[p] = struct{}{}
	}
	b.cases[caseID] = &cse{
		brief:      brief,
		escrowID:   escrowID,
		panel:      panel,
		panelSet:   set,
		commits:    map[string]string{},
		reveals:    map[string]string{},
		deadline:   deadline,
		instance:   PanelInstanceFirst,
		generation: PanelGenerationFirst,
	}
	b.byEscrow[escrowID] = caseID
	b.flushLocked()
	return brief, nil
}

func (b *Book) Brief(caseID string) (Brief, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.cases[caseID]
	if !ok {
		return Brief{}, false
	}
	out := c.brief
	out.RevealsOpen = c.frozen
	if !c.frozen {
		out.RevealsOpen = false
	}
	return out, true
}

func (b *Book) Panel(caseID string) ([]string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.cases[caseID]
	if !ok {
		return nil, false
	}
	return append([]string(nil), c.panel...), true
}

func (b *Book) BriefsFor(addr string) []Brief {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []Brief{}
	for _, c := range b.cases {
		if _, on := c.panelSet[addr]; on {
			out = append(out, c.brief)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CaseID < out[j].CaseID })
	return out
}

func (b *Book) Commit(caseID, key, verdictHash string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.cases[caseID]
	if !ok {
		return fmt.Errorf("unknown case")
	}
	if _, on := c.panelSet[key]; !on {
		return fmt.Errorf("not on the panel")
	}
	if _, exists := c.commits[key]; exists {
		return fmt.Errorf("already committed")
	}
	if verdictHash == "" {
		return fmt.Errorf("verdict hash required")
	}
	c.commits[key] = verdictHash
	b.flushLocked()
	return nil
}

func (b *Book) Reveal(caseID, key, verdict string) error {
	return b.RevealNonce(caseID, key, verdict, "")
}

func (b *Book) RevealNonce(caseID, key, verdict, nonce string) error {
	if verdict != VerdictFulfilled && verdict != VerdictNotFulfilled {
		return fmt.Errorf("verdict must be fulfilled or not-fulfilled")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.cases[caseID]
	if !ok {
		return fmt.Errorf("unknown case")
	}
	if _, on := c.panelSet[key]; !on {
		return fmt.Errorf("not on the panel")
	}
	want, committed := c.commits[key]
	if !committed || want != HashCommit(verdict, nonce) {
		return fmt.Errorf("reveal does not match commit")
	}
	if _, done := c.reveals[key]; done {
		return fmt.Errorf("already revealed")
	}
	c.reveals[key] = verdict
	if c.frozen && c.quorum == "" {
		if outcome, ok := quorumOf(c.reveals); ok {
			c.quorum = outcome
		}
	}
	b.flushLocked()
	return nil
}

func (b *Book) Reveals(caseID string) (map[string]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.cases[caseID]
	if !ok {
		return nil, fmt.Errorf("unknown case")
	}
	if !c.frozen {
		return map[string]string{}, nil
	}
	out := map[string]string{}
	for k, v := range c.reveals {
		out[k] = v
	}
	return out, nil
}

func (b *Book) Quorum(caseID string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.cases[caseID]
	if !ok {
		return "", false
	}
	if !c.frozen {
		if b.height > c.deadline {
			c.frozen = true
		}
	}
	if !c.frozen {
		return "", false
	}
	if c.quorum == "" {
		if q, ok := quorumOf(c.reveals); ok {
			c.quorum = q
		}
	}
	if c.quorum == "" {
		return "", false
	}
	return coreOutcome(c.quorum), true
}

func (b *Book) TimeoutAllowed(caseID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.cases[caseID]
	if !ok {
		return fmt.Errorf("unknown case")
	}
	if c.quorum == "" && c.frozen {
		return fmt.Errorf("timeout refused: dispute has no quorum")
	}
	if c.quorum == "" {
		return fmt.Errorf("timeout refused: dispute has no quorum")
	}
	return nil
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

func HashVerdict(verdict string) string { return HashCommit(verdict, "") }

func HashCommit(verdict, nonce string) string {
	var sum [32]byte
	if nonce == "" {
		sum = sha256.Sum256([]byte(verdict))
	} else {
		sum = sha256.Sum256([]byte(verdict + "\x00" + nonce))
	}
	return hex.EncodeToString(sum[:])
}

func coreOutcome(v string) string {
	switch v {
	case VerdictFulfilled:
		return OutcomeFulfilled
	case VerdictNotFulfilled:
		return OutcomeNotFulfilled
	default:
		return v
	}
}

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

// PanelSeed is 89 bytes: domain || sha256(escrow) || sha256(terms) || instance || generation.
func PanelSeed(escrowID, terms string, instance, generation byte) []byte {
	out := make([]byte, 89)
	copy(out[:23], []byte(PanelDomain))
	esc := sha256.Sum256([]byte(escrowID))
	copy(out[23:55], esc[:])
	tm := sha256.Sum256([]byte(terms))
	copy(out[55:87], tm[:])
	out[87] = instance
	out[88] = generation
	return out
}

// PickFive seats by unsigned remainder over the remaining bonded set.
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
	sort.Strings(eligible)
	if len(eligible) < 5 {
		return nil, fmt.Errorf("fewer than five eligible volunteers")
	}
	remaining := append([]string(nil), eligible...)
	picked := make([]string, 0, 5)
	for seat := 0; seat < 5; seat++ {
		var rest [8]byte
		binary.BigEndian.PutUint64(rest[:], uint64(seat))
		h := sha256.Sum256(append([]byte(seed), rest[:]...))
		idx := int(binary.BigEndian.Uint64(h[:8]) % uint64(len(remaining)))
		picked = append(picked, remaining[idx])
		remaining = append(remaining[:idx], remaining[idx+1:]...)
	}
	return picked, nil
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

func parseUint(s string) uint64 {
	var n uint64
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + uint64(c-'0')
	}
	return n
}

type persistSnap struct {
	Roster []string      `json:"roster"`
	Height uint64        `json:"height"`
	Cases  []persistCase `json:"cases"`
}

type persistCase struct {
	Brief      Brief             `json:"brief"`
	EscrowID   string            `json:"escrowId"`
	Panel      []string          `json:"panel"`
	Commits    map[string]string `json:"commits"`
	Reveals    map[string]string `json:"reveals"`
	Quorum     string            `json:"quorum"`
	Deadline   uint64            `json:"deadline"`
	Frozen     bool              `json:"frozen"`
	Instance   byte              `json:"instance"`
	Generation byte              `json:"generation"`
}

func (c *cse) persist() persistCase {
	return persistCase{
		Brief:      c.brief,
		EscrowID:   c.escrowID,
		Panel:      c.panel,
		Commits:    c.commits,
		Reveals:    c.reveals,
		Quorum:     c.quorum,
		Deadline:   c.deadline,
		Frozen:     c.frozen,
		Instance:   c.instance,
		Generation: c.generation,
	}
}

func (p persistCase) toCase() *cse {
	set := map[string]struct{}{}
	for _, k := range p.Panel {
		set[k] = struct{}{}
	}
	if p.Commits == nil {
		p.Commits = map[string]string{}
	}
	if p.Reveals == nil {
		p.Reveals = map[string]string{}
	}
	return &cse{
		brief:      p.Brief,
		escrowID:   p.EscrowID,
		panel:      p.Panel,
		panelSet:   set,
		commits:    p.Commits,
		reveals:    p.Reveals,
		quorum:     p.Quorum,
		deadline:   p.Deadline,
		frozen:     p.Frozen,
		instance:   p.Instance,
		generation: p.Generation,
	}
}

func (b *Book) flushLocked() {
	if b.path == "" {
		return
	}
	snap := persistSnap{Roster: b.roster, Height: b.height}
	for _, c := range b.cases {
		snap.Cases = append(snap.Cases, c.persist())
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return
	}
	dir := filepath.Dir(b.path)
	tmp, err := os.CreateTemp(dir, "volunteers-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return
	}
	_ = os.Rename(tmpName, b.path)
}
