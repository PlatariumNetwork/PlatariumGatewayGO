package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"platarium-gateway-go/internal/blockchain"
	"platarium-gateway-go/internal/deals"
	"platarium-gateway-go/internal/escrow"

	"github.com/gorilla/mux"
)

// requireDealGuarantor rejects a deal lock whose node is not this gateway's operator wallet.
// Other transaction kinds are left unchanged. The caller cannot choose the guarantor.
func requireDealGuarantor(h *Handler, tx *blockchain.Transaction) error {
	if tx == nil || tx.Purpose != escrow.PurposeDeal || tx.Type != escrow.TxKindLock {
		return nil
	}
	node := strings.TrimSpace(h.operatorWallet)
	if node == "" {
		return fmt.Errorf("this node has no guarantor address")
	}
	if !strings.EqualFold(strings.TrimSpace(tx.SettleNode), node) {
		return fmt.Errorf("guarantor is assigned by the node")
	}
	return nil
}

// signedDealAdmit is the mempool handoff for an already-signed deal transaction.
// Tests replace it so admission can be observed without a Core process.
var signedDealAdmit = func(h *Handler, tx *blockchain.Transaction) error {
	return h.admitToMempool(tx)
}

func (h *Handler) deals() *deals.Book {
	if h.dealBook == nil {
		h.dealBook = deals.NewBook()
	}
	return h.dealBook
}

func (h *Handler) coreStatePath() string {
	if h.dealStateFile != "" {
		return h.dealStateFile
	}
	if h.blockchain != nil && h.blockchain.Ledger() != nil {
		return h.blockchain.Ledger().StateFilePath()
	}
	return ""
}

type signedDealBody struct {
	Mnemonic     string          `json:"mnemonic"`
	Alphanumeric string          `json:"alphanumeric"`
	Transaction  json.RawMessage `json:"transaction"`
	CaseID       string          `json:"caseId"`
}

func rejectSecretMaterial(body signedDealBody) string {
	if strings.TrimSpace(body.Mnemonic) != "" || strings.TrimSpace(body.Alphanumeric) != "" {
		return "mnemonic is not accepted on the signed deal path"
	}
	return ""
}

// SubmitDealLock POST /api/deals/lock admits a Core-signed escrow_lock with purpose deal.
func (h *Handler) SubmitDealLock(w http.ResponseWriter, r *http.Request) {
	var body signedDealBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if msg := rejectSecretMaterial(body); msg != "" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	tx, err := parseSignedEscrowTx(string(body.Transaction), escrow.TxKindLock)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if tx.Type != escrow.TxKindLock || tx.Purpose != escrow.PurposeDeal {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "signed lock must use purpose deal"})
		return
	}
	if err := requireDealGuarantor(h, tx); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if tx.Asset != "PLP" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "deal lock asset must be PLP"})
		return
	}
	if err := signedDealAdmit(h, tx); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"escrowId": tx.EscrowID,
		"hash":     tx.Hash,
	})
}

// SubmitDealSettle POST /api/deals/settle admits a Core-signed escrow_settle.
// Gateway does not recompute the fee split; the signed outcome is admitted as-is.
func (h *Handler) SubmitDealSettle(w http.ResponseWriter, r *http.Request) {
	h.admitDealSettle(w, r, false)
}

// SubmitDealDisputeSettle POST /api/deals/dispute-settle admits a settle only after a reveal quorum.
func (h *Handler) SubmitDealDisputeSettle(w http.ResponseWriter, r *http.Request) {
	h.admitDealSettle(w, r, true)
}

func (h *Handler) admitDealSettle(w http.ResponseWriter, r *http.Request, dispute bool) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	var body signedDealBody
	if err := json.Unmarshal(raw, &body); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if msg := rejectSecretMaterial(body); msg != "" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	tx, err := parseSignedEscrowTx(string(body.Transaction), escrow.TxKindSettle)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if tx.Type != escrow.TxKindSettle || tx.Purpose != escrow.PurposeDeal {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "signed settle must use purpose deal"})
		return
	}
	if dispute {
		outcome, ok := h.deals().Quorum(body.CaseID)
		if !ok || outcome != tx.SettleOutcomeKey {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "dispute quorum does not match this settle"})
			return
		}
	}
	if err := signedDealAdmit(h, tx); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"escrowId":   tx.EscrowID,
		"outcomeKey": tx.SettleOutcomeKey,
		"hash":       tx.Hash,
	})
}

func (h *Handler) ListDeals(w http.ResponseWriter, r *http.Request) {
	path := h.coreStatePath()
	if path == "" {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "core state unavailable"})
		return
	}
	rows, err := deals.LoadCoreEscrows(path)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	address := r.URL.Query().Get("address")
	jsonResponse(w, http.StatusOK, map[string]interface{}{"deals": deals.DealsForAddress(rows, address)})
}

func (h *Handler) GetDeal(w http.ResponseWriter, r *http.Request) {
	path := h.coreStatePath()
	if path == "" {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "core state unavailable"})
		return
	}
	rows, err := deals.LoadCoreEscrows(path)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	id := mux.Vars(r)["id"]
	row, ok := deals.DealByID(rows, id)
	if !ok {
		jsonResponse(w, http.StatusNotFound, map[string]string{"error": "deal not found"})
		return
	}
	jsonResponse(w, http.StatusOK, row)
}

func (h *Handler) OpenDealDispute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Terms       string   `json:"terms"`
		OpenBlock   string   `json:"openBlock"`
		EscrowID    string   `json:"escrowId"`
		Bonded      []string `json:"bonded"`
		Creator     string   `json:"creator"`
		Beneficiary string   `json:"beneficiary"`
		Node        string   `json:"node"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	brief, err := h.deals().Open(body.Terms, body.OpenBlock, body.EscrowID, body.Bonded, []string{body.Creator, body.Beneficiary, body.Node})
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, brief)
}

func (h *Handler) GetVolunteerBrief(w http.ResponseWriter, r *http.Request) {
	brief, ok := h.deals().Brief(mux.Vars(r)["caseId"])
	if !ok {
		jsonResponse(w, http.StatusNotFound, map[string]string{"error": "case not found"})
		return
	}
	jsonResponse(w, http.StatusOK, brief)
}

func (h *Handler) CommitVolunteer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key         string `json:"key"`
		VerdictHash string `json:"verdictHash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if err := h.deals().Commit(mux.Vars(r)["caseId"], body.Key, body.VerdictHash); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) RevealVolunteer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key     string `json:"key"`
		Verdict string `json:"verdict"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if err := h.deals().Reveal(mux.Vars(r)["caseId"], body.Key, body.Verdict); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]bool{"ok": true})
}
