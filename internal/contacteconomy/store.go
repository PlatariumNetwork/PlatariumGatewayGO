// Package contacteconomy: Gateway-side first-contact messaging protocol state.
// Stores encrypted request routing metadata and protocol contacts — never plaintext
// bodies or on-chain social graph.

package contacteconomy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	StatusPending     = "pending"
	StatusEstablished = "established"
	StatusRejected    = "rejected"
	StatusExpired     = "expired"

	OutcomeAccepted = "accepted"
	OutcomeTimeout  = "timeout"
	OutcomeRejected = "rejected"
)

// Config holds protocol clamps and settlement defaults (env-overridable).
type Config struct {
	Enabled            bool
	MinFeeUplp         uint64
	MaxFeeUplp         uint64
	DefaultUnknownUplp uint64
	DefaultVerifiedUplp uint64
	TimeoutSecs        int64
	BasePendingLimit   int
	EconomyGateDMs     bool
}

func ConfigFromEnv() Config {
	return Config{
		Enabled:             envBool("PLATARIUM_CONTACT_ECONOMY", true),
		MinFeeUplp:          envU64("PLATARIUM_CONTACT_MIN_FEE_UPLP", 10_000),       // 0.01 PLP
		MaxFeeUplp:          envU64("PLATARIUM_CONTACT_MAX_FEE_UPLP", 100_000_000), // 100 PLP
		DefaultUnknownUplp:  envU64("PLATARIUM_CONTACT_DEFAULT_UNKNOWN_UPLP", 1_000_000),
		DefaultVerifiedUplp: envU64("PLATARIUM_CONTACT_DEFAULT_VERIFIED_UPLP", 100_000),
		TimeoutSecs:         envI64("PLATARIUM_CONTACT_TIMEOUT_SECS", 30*24*3600),
		BasePendingLimit:    int(envU64("PLATARIUM_CONTACT_BASE_PENDING", 5)),
		EconomyGateDMs:      envBool("PLATARIUM_CONTACT_GATE_DMS", true),
	}
}

func envBool(k string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

func envU64(k string, def uint64) uint64 {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	var n uint64
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return def
	}
	return n
}

func envI64(k string, def int64) int64 {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	var n int64
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return def
	}
	return n
}

// PricingAnnounce is a signed fee schedule for an address.
type PricingAnnounce struct {
	Address         string `json:"address"`
	UnknownFeeUplp  uint64 `json:"unknownFeeUplp"`
	VerifiedFeeUplp uint64 `json:"verifiedFeeUplp"`
	Blocked         bool   `json:"blocked"`
	Signature       string `json:"signature,omitempty"`
	UpdatedAt       int64  `json:"updatedAt"`
}

// ContactRequest is encrypted first-contact request metadata (ciphertext only).
type ContactRequest struct {
	RequestID         string `json:"requestId"`
	RequestIDHash     string `json:"requestIdHash"`
	Sender            string `json:"sender"`
	Receiver          string `json:"receiver"`
	SenderPubKey      string `json:"senderPublicKey"`
	ReceiverPubKey    string `json:"receiverPublicKey"`
	EncryptedPayload  string `json:"encryptedPayload"`
	Timestamp         int64  `json:"timestamp"`
	ExpiresAt         int64  `json:"expiresAt"`
	LockTxHash        string `json:"lockTxHash"`
	AmountUplp        uint64 `json:"amountUplp"`
	Status            string `json:"status"`
	SettleOutcome     string `json:"settleOutcome,omitempty"`
	SettleTxHash      string `json:"settleTxHash,omitempty"`
}

// ProtocolContactPair is an unordered established protocol relationship.
type ProtocolContactPair struct {
	A           string `json:"a"`
	B           string `json:"b"`
	Established int64  `json:"establishedAt"`
	RequestID   string `json:"requestId"`
}

// Reputation tracks messenger capability XP (not farmable by raw PLP transfers).
type Reputation struct {
	Address string `json:"address"`
	XP      uint64 `json:"xp"`
}

type filePayload struct {
	Requests  map[string]ContactRequest     `json:"requests"`
	Contacts  map[string]ProtocolContactPair `json:"contacts"`
	Pricing   map[string]PricingAnnounce    `json:"pricing"`
	XP        map[string]Reputation         `json:"xp"`
}

// Store persists protocol contact state on the gateway node.
type Store struct {
	path   string
	cfg    Config
	mu     sync.RWMutex
	data   filePayload
}

func NewStore(path string, cfg Config) (*Store, error) {
	if path == "" {
		path = "data/contact-economy.json"
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create contact economy dir: %w", err)
		}
	}
	s := &Store{
		path: path,
		cfg:  cfg,
		data: filePayload{
			Requests: make(map[string]ContactRequest),
			Contacts: make(map[string]ProtocolContactPair),
			Pricing:  make(map[string]PricingAnnounce),
			XP:       make(map[string]Reputation),
		},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Config() Config { return s.cfg }

func normalize(addr string) string {
	return strings.TrimSpace(addr)
}

func pairKey(a, b string) string {
	a, b = normalize(a), normalize(b)
	if strings.ToLower(a) > strings.ToLower(b) {
		a, b = b, a
	}
	return strings.ToLower(a) + "|" + strings.ToLower(b)
}

// RequestIDHash returns sha256 hex of requestId (opaque chain key).
func RequestIDHash(requestID string) string {
	sum := sha256.Sum256([]byte(requestID))
	return hex.EncodeToString(sum[:])
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read contact economy: %w", err)
	}
	var payload filePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("parse contact economy: %w", err)
	}
	if payload.Requests == nil {
		payload.Requests = make(map[string]ContactRequest)
	}
	if payload.Contacts == nil {
		payload.Contacts = make(map[string]ProtocolContactPair)
	}
	if payload.Pricing == nil {
		payload.Pricing = make(map[string]PricingAnnounce)
	}
	if payload.XP == nil {
		payload.XP = make(map[string]Reputation)
	}
	s.data = payload
	return nil
}

func (s *Store) persistLocked() error {
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) ClampFee(uplp uint64) uint64 {
	if uplp < s.cfg.MinFeeUplp {
		return s.cfg.MinFeeUplp
	}
	if uplp > s.cfg.MaxFeeUplp {
		return s.cfg.MaxFeeUplp
	}
	return uplp
}

func (s *Store) SetPricing(p PricingAnnounce) (PricingAnnounce, error) {
	addr := normalize(p.Address)
	if addr == "" {
		return PricingAnnounce{}, fmt.Errorf("address required")
	}
	p.Address = addr
	p.UnknownFeeUplp = s.ClampFee(p.UnknownFeeUplp)
	p.VerifiedFeeUplp = s.ClampFee(p.VerifiedFeeUplp)
	p.UpdatedAt = time.Now().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Pricing[strings.ToLower(addr)] = p
	if err := s.persistLocked(); err != nil {
		return PricingAnnounce{}, err
	}
	return p, nil
}

func (s *Store) GetPricing(address string) PricingAnnounce {
	addr := normalize(address)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.data.Pricing[strings.ToLower(addr)]; ok {
		return p
	}
	return PricingAnnounce{
		Address:         addr,
		UnknownFeeUplp:  s.cfg.DefaultUnknownUplp,
		VerifiedFeeUplp: s.cfg.DefaultVerifiedUplp,
		Blocked:         false,
	}
}

func (s *Store) HasProtocolContact(a, b string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data.Contacts[pairKey(a, b)]
	return ok
}

func (s *Store) PendingOutboundCount(sender string) int {
	sender = normalize(sender)
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, r := range s.data.Requests {
		if strings.EqualFold(r.Sender, sender) && r.Status == StatusPending {
			n++
		}
	}
	return n
}

func (s *Store) GetXP(address string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if r, ok := s.data.XP[strings.ToLower(normalize(address))]; ok {
		return r.XP
	}
	return 0
}

func (s *Store) PendingLimit(address string) int {
	xp := s.GetXP(address)
	bonus := int(xp / 100) // +1 pending per 100 XP
	lim := s.cfg.BasePendingLimit + bonus
	if lim > 50 {
		lim = 50
	}
	return lim
}

func (s *Store) AddXP(address string, delta uint64) {
	addr := strings.ToLower(normalize(address))
	if addr == "" || delta == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.data.XP[addr]
	r.Address = normalize(address)
	r.XP += delta
	s.data.XP[addr] = r
	_ = s.persistLocked()
}

func (s *Store) CreateRequest(req ContactRequest) (ContactRequest, error) {
	if !s.cfg.Enabled {
		return ContactRequest{}, fmt.Errorf("contact economy disabled")
	}
	req.Sender = normalize(req.Sender)
	req.Receiver = normalize(req.Receiver)
	if req.RequestID == "" || req.Sender == "" || req.Receiver == "" {
		return ContactRequest{}, fmt.Errorf("requestId, sender, receiver required")
	}
	if req.LockTxHash == "" || req.EncryptedPayload == "" {
		return ContactRequest{}, fmt.Errorf("lockTxHash and encryptedPayload required")
	}
	// Reject placeholder receipts — economic anti-spam requires a real chain/mempool tx hash.
	if strings.HasPrefix(strings.ToLower(req.LockTxHash), "escrow_lock:") ||
		strings.HasPrefix(strings.ToLower(req.LockTxHash), "escrow-intent:") {
		return ContactRequest{}, fmt.Errorf("lockTxHash must be a real escrow_lock transaction hash")
	}
	if len(req.LockTxHash) < 32 {
		return ContactRequest{}, fmt.Errorf("lockTxHash too short")
	}
	// Ciphertext size gate (E2EE — no ML on plaintext at Gateway).
	if len(req.EncryptedPayload) > 64*1024 {
		return ContactRequest{}, fmt.Errorf("encrypted payload too large")
	}
	if req.AmountUplp == 0 {
		return ContactRequest{}, fmt.Errorf("amountUplp required")
	}
	pricing := s.GetPricing(req.Receiver)
	if pricing.Blocked {
		return ContactRequest{}, fmt.Errorf("receiver has disabled unknown contacts")
	}
	if s.HasProtocolContact(req.Sender, req.Receiver) {
		return ContactRequest{}, fmt.Errorf("protocol contact already established")
	}
	if s.PendingOutboundCount(req.Sender) >= s.PendingLimit(req.Sender) {
		return ContactRequest{}, fmt.Errorf("pending contact request limit exceeded")
	}

	now := time.Now().Unix()
	req.RequestIDHash = RequestIDHash(req.RequestID)
	if req.Timestamp == 0 {
		req.Timestamp = now
	}
	req.ExpiresAt = req.Timestamp + s.cfg.TimeoutSecs
	req.Status = StatusPending

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.data.Requests[req.RequestID]; exists {
		return ContactRequest{}, fmt.Errorf("duplicate requestId")
	}
	// Anti-farm: same lockTxHash cannot back multiple requests
	for _, existing := range s.data.Requests {
		if existing.LockTxHash == req.LockTxHash {
			return ContactRequest{}, fmt.Errorf("lockTxHash already used")
		}
	}
	// Sender/receiver cooldown: one pending pair at a time
	for _, existing := range s.data.Requests {
		if existing.Status != StatusPending {
			continue
		}
		if strings.EqualFold(existing.Sender, req.Sender) && strings.EqualFold(existing.Receiver, req.Receiver) {
			return ContactRequest{}, fmt.Errorf("pending request already exists for this pair")
		}
	}
	s.data.Requests[req.RequestID] = req
	if err := s.persistLocked(); err != nil {
		return ContactRequest{}, err
	}
	return req, nil
}

func (s *Store) GetRequest(requestID string) (ContactRequest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.data.Requests[requestID]
	return r, ok
}

// FindByEscrowID looks up a request by opaque escrow id (requestIdHash).
func (s *Store) FindByEscrowID(escrowID string) (ContactRequest, bool) {
	id := strings.TrimSpace(escrowID)
	if id == "" {
		return ContactRequest{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.data.Requests {
		if r.RequestIDHash == id || strings.EqualFold(r.RequestIDHash, id) {
			return r, true
		}
	}
	return ContactRequest{}, false
}

func (s *Store) ListPendingFor(address string) []ContactRequest {
	addr := normalize(address)
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ContactRequest, 0)
	for _, r := range s.data.Requests {
		if r.Status != StatusPending {
			continue
		}
		if strings.EqualFold(r.Receiver, addr) || strings.EqualFold(r.Sender, addr) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp < out[j].Timestamp })
	return out
}

// RespondAcceptOrReject marks request and establishes protocol contact on accept.
// Ownership: either (1) mnemonic+alphanumeric deriving actor address, verified by caller before
// calling Respond, or (2) a Core-verified signature hex passed as signature with pubKey.
// signature must be non-empty; if it starts with "owned:" the Gateway already verified wallet ownership.
func (s *Store) Respond(requestID, actor, outcome, signature string) (ContactRequest, error) {
	if strings.TrimSpace(signature) == "" {
		return ContactRequest{}, fmt.Errorf("signature required")
	}
	// Reject legacy messenger stub signatures.
	if strings.HasPrefix(signature, "sig-") && !strings.HasPrefix(signature, "sig-core:") {
		return ContactRequest{}, fmt.Errorf("invalid signature: use wallet ownership proof")
	}
	actor = normalize(actor)
	s.mu.Lock()
	defer s.mu.Unlock()
	req, ok := s.data.Requests[requestID]
	if !ok {
		return ContactRequest{}, fmt.Errorf("request not found")
	}
	if req.Status != StatusPending {
		return ContactRequest{}, fmt.Errorf("request not pending")
	}
	if !strings.EqualFold(req.Receiver, actor) {
		return ContactRequest{}, fmt.Errorf("only receiver may respond")
	}
	now := time.Now().Unix()
	if now > req.ExpiresAt {
		req.Status = StatusExpired
		req.SettleOutcome = OutcomeTimeout
		s.data.Requests[requestID] = req
		_ = s.persistLocked()
		return req, fmt.Errorf("request expired")
	}
	switch outcome {
	case OutcomeAccepted:
		req.Status = StatusEstablished
		req.SettleOutcome = OutcomeAccepted
		s.data.Contacts[pairKey(req.Sender, req.Receiver)] = ProtocolContactPair{
			A:           req.Sender,
			B:           req.Receiver,
			Established: now,
			RequestID:   req.RequestID,
		}
	case OutcomeRejected:
		req.Status = StatusRejected
		req.SettleOutcome = OutcomeRejected
	default:
		return ContactRequest{}, fmt.Errorf("invalid outcome")
	}
	s.data.Requests[requestID] = req
	if err := s.persistLocked(); err != nil {
		return ContactRequest{}, err
	}
	return req, nil
}

func (s *Store) MarkSettled(requestID, settleTxHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	req, ok := s.data.Requests[requestID]
	if !ok {
		return fmt.Errorf("request not found")
	}
	req.SettleTxHash = settleTxHash
	s.data.Requests[requestID] = req
	return s.persistLocked()
}

// ExpireDue marks timed-out pending requests and returns them for settlement.
func (s *Store) ExpireDue(now int64) []ContactRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ContactRequest, 0)
	for id, req := range s.data.Requests {
		if req.Status != StatusPending {
			continue
		}
		if now <= req.ExpiresAt {
			continue
		}
		req.Status = StatusExpired
		req.SettleOutcome = OutcomeTimeout
		s.data.Requests[id] = req
		out = append(out, req)
	}
	if len(out) > 0 {
		_ = s.persistLocked()
	}
	return out
}

// CanSendFreeDM returns true when protocol contact exists or economy gate is off.
func (s *Store) CanSendFreeDM(from, to string) bool {
	if !s.cfg.Enabled || !s.cfg.EconomyGateDMs {
		return true
	}
	if strings.EqualFold(normalize(from), normalize(to)) {
		return true
	}
	return s.HasProtocolContact(from, to)
}
