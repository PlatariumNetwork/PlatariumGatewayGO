package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"platarium-gateway-go/internal/contacteconomy"
	"platarium-gateway-go/internal/escrow"
	"platarium-gateway-go/internal/protocol"

	"github.com/gorilla/mux"
)

func ensureContactEconomy(h *Handler) {
	path := strings.TrimSpace(os.Getenv("PLATARIUM_CONTACT_ECONOMY_FILE"))
	if path == "" {
		path = "data/contact-economy.json"
	}
	cfg := contacteconomy.ConfigFromEnv()
	store, err := contacteconomy.NewStore(path, cfg)
	if err != nil {
		log.Printf("[WARN] contact economy store: %v", err)
		return
	}
	h.contactEconomy = store
	if h.wsServer != nil {
		h.wsServer.SetContactEconomy(store)
	}
	log.Printf("[INFO] Contact economy ready (enabled=%v gateDMs=%v)", cfg.Enabled, cfg.EconomyGateDMs)
	go h.runContactTimeoutSweeper()
}

func (h *Handler) runContactTimeoutSweeper() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		if h.contactEconomy == nil {
			continue
		}
		expired := h.contactEconomy.ExpireDue(time.Now().Unix())
		for _, req := range expired {
			intent := protocol.ContactSettleFromRequest(req, "")
			log.Printf("[CONTACT] expired request %s escrow=%s — client/operator must submit %s outcome=%s",
				req.RequestID, intent.EscrowID, intent.TxKind, intent.OutcomeKey)
		}
	}
}

// GetContactPricing GET /api/contact/pricing?address=Px…
func (h *Handler) GetContactPricing(w http.ResponseWriter, r *http.Request) {
	if h.contactEconomy == nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "contact economy unavailable"})
		return
	}
	addr := strings.TrimSpace(r.URL.Query().Get("address"))
	if addr == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "address required"})
		return
	}
	p := h.contactEconomy.GetPricing(addr)
	cfg := h.contactEconomy.Config()
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"pricing": p,
		"protocol": map[string]interface{}{
			"minFeeUplp": cfg.MinFeeUplp,
			"maxFeeUplp": cfg.MaxFeeUplp,
			"timeoutSecs": cfg.TimeoutSecs,
		},
	})
}

// SetContactPricing POST /api/contact/pricing
func (h *Handler) SetContactPricing(w http.ResponseWriter, r *http.Request) {
	if h.contactEconomy == nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "contact economy unavailable"})
		return
	}
	var body contacteconomy.PricingAnnounce
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	p, err := h.contactEconomy.SetPricing(body)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"pricing": p})
}

// QueryProtocolContact GET /api/contact/protocol?a=&b=
func (h *Handler) QueryProtocolContact(w http.ResponseWriter, r *http.Request) {
	if h.contactEconomy == nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"established": true, "economy": false})
		return
	}
	a := strings.TrimSpace(r.URL.Query().Get("a"))
	b := strings.TrimSpace(r.URL.Query().Get("b"))
	if a == "" || b == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "a and b required"})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"established": h.contactEconomy.HasProtocolContact(a, b),
		"economy":     h.contactEconomy.Config().Enabled,
	})
}

// CreateContactRequest POST /api/contact/request
func (h *Handler) CreateContactRequest(w http.ResponseWriter, r *http.Request) {
	if !h.allowContactRate(r, "contact_request") {
		jsonResponse(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
		return
	}
	if h.contactEconomy == nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "contact economy unavailable"})
		return
	}
	var body contacteconomy.ContactRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if err := h.verifyEscrowLockTx(body.LockTxHash, body.Sender, body.AmountUplp, contacteconomy.RequestIDHash(body.RequestID)); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	req, err := h.contactEconomy.CreateRequest(body)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// Deliver encrypted payload to receiver via WS (ciphertext only).
	if h.wsServer != nil {
		h.wsServer.DeliverContactRequest(req)
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"request": req})
}

// RespondContactRequest POST /api/contact/respond
func (h *Handler) RespondContactRequest(w http.ResponseWriter, r *http.Request) {
	if !h.allowContactRate(r, "contact_respond") {
		jsonResponse(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
		return
	}
	if h.contactEconomy == nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "contact economy unavailable"})
		return
	}
	var body struct {
		RequestID         string `json:"requestId"`
		Actor             string `json:"actor"`
		Outcome           string `json:"outcome"`
		Signature         string `json:"signature"`
		EncryptedResponse string `json:"encryptedResponse,omitempty"`
		Mnemonic          string `json:"mnemonic,omitempty"`
		Alphanumeric      string `json:"alphanumeric,omitempty"`
		PubMain           string `json:"pubMain,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	proof, err := h.verifyContactRespondOwnership(body.Actor, body.RequestID, body.Outcome, body.Signature, body.Mnemonic, body.Alphanumeric, body.PubMain)
	if err != nil {
		jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	req, err := h.contactEconomy.Respond(body.RequestID, body.Actor, body.Outcome, proof)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.Outcome == contacteconomy.OutcomeAccepted {
		h.contactEconomy.AddXP(body.Actor, 25)
		h.contactEconomy.AddXP(req.Sender, 10)
	} else if body.Outcome == contacteconomy.OutcomeRejected {
		h.contactEconomy.AddXP(body.Actor, 1)
	}
	if h.wsServer != nil {
		h.wsServer.NotifyContactResolved(req, body.EncryptedResponse)
	}
	settleIntent := protocol.ContactSettleFromRequest(req, "")
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"request":      req,
		"settleIntent": settleIntent,
	})
}

// ListContactRequests GET /api/contact/requests?address=
func (h *Handler) ListContactRequests(w http.ResponseWriter, r *http.Request) {
	if h.contactEconomy == nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "contact economy unavailable"})
		return
	}
	addr := strings.TrimSpace(r.URL.Query().Get("address"))
	if addr == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "address required"})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"requests":     h.contactEconomy.ListPendingFor(addr),
		"pendingLimit": h.contactEconomy.PendingLimit(addr),
		"xp":           h.contactEconomy.GetXP(addr),
	})
}

// GetContactRequestStatus GET /api/contact/request/{id}
func (h *Handler) GetContactRequestStatus(w http.ResponseWriter, r *http.Request) {
	if h.contactEconomy == nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "contact economy unavailable"})
		return
	}
	id := strings.TrimSpace(mux.Vars(r)["id"])
	if id == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "id required"})
		return
	}
	req, ok := h.contactEconomy.GetRequest(id)
	if !ok {
		jsonResponse(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"request": req})
}

// verifyEscrowLockTx ensures lockTxHash refers to a mempool/chain escrow_lock matching sender/amount/escrowId.
func (h *Handler) verifyEscrowLockTx(lockTxHash, sender string, amountUplp uint64, escrowID string) error {
	hash := strings.TrimSpace(lockTxHash)
	if hash == "" {
		return fmt.Errorf("lockTxHash required")
	}
	if h.blockchain == nil {
		return fmt.Errorf("blockchain unavailable for lock verification")
	}
	tx := h.blockchain.GetTransaction(hash)
	if tx == nil {
		for _, m := range h.blockchain.GetMempool() {
			if m != nil && strings.EqualFold(m.Hash, hash) {
				tx = m
				break
			}
		}
	}
	if tx == nil {
		return fmt.Errorf("lock tx not found in mempool/chain — submit escrow_lock first")
	}
	kind := strings.ToLower(tx.Type)
	if kind != escrow.TxKindLock && kind != "contact_escrow_lock" {
		return fmt.Errorf("tx is not escrow_lock (got %s)", tx.Type)
	}
	if sender != "" && !strings.EqualFold(tx.From, sender) {
		return fmt.Errorf("lock tx from mismatch")
	}
	if amountUplp > 0 && tx.AmountUplp != 0 && tx.AmountUplp != amountUplp {
		return fmt.Errorf("lock amount mismatch")
	}
	eid := tx.EscrowID
	if eid == "" {
		eid = tx.RequestIDHash
	}
	if escrowID != "" && eid != "" && !strings.EqualFold(eid, escrowID) {
		return fmt.Errorf("escrow id mismatch")
	}
	return nil
}

// verifyContactRespondOwnership proves actor controls the wallet.
func (h *Handler) verifyContactRespondOwnership(
	actor, requestID, outcome, signature, mnemonic, alphanumeric, pubMain string,
) (string, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return "", fmt.Errorf("actor required")
	}
	if mnemonic != "" && alphanumeric != "" {
		if h.rustCore == nil {
			return "", fmt.Errorf("core unavailable for ownership proof")
		}
		keys, err := h.rustCore.GenerateKeys(mnemonic, alphanumeric, 0)
		if err != nil {
			return "", fmt.Errorf("GenerateKeys: %w", err)
		}
		pk := keys["publicKey"]
		if !strings.EqualFold(pk, actor) {
			return "", fmt.Errorf("mnemonic does not match actor address")
		}
		return "owned:" + pk, nil
	}
	if strings.HasPrefix(signature, "owned:") {
		return signature, nil
	}
	if strings.HasPrefix(signature, "sig-core:") && h.rustCore != nil {
		sigHex := strings.TrimPrefix(signature, "sig-core:")
		pub := pubMain
		if pub == "" {
			pub = actor
		}
		msg := map[string]interface{}{
			"type":      "contact_respond",
			"requestId": requestID,
			"outcome":   outcome,
			"actor":     actor,
		}
		ok, err := h.rustCore.VerifySignature(msg, sigHex, pub)
		if err != nil {
			return "", fmt.Errorf("signature verify: %w", err)
		}
		if !ok {
			return "", fmt.Errorf("invalid contact respond signature")
		}
		return signature, nil
	}
	return "", fmt.Errorf("provide mnemonic+alphanumeric ownership proof or sig-core signature")
}
