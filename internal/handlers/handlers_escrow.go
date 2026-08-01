package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"platarium-gateway-go/internal/escrow"
	"platarium-gateway-go/internal/protocol"

	"github.com/gorilla/mux"
)

// GetEscrowStatus GET /api/escrow/{id}
// Queries protocol-known escrow metadata (contact purpose today). Gateway does not
// recompute settlement splits — clients submit escrow_settle to Core.
func (h *Handler) GetEscrowStatus(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(mux.Vars(r)["id"])
	if id == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "escrow id required"})
		return
	}
	if h.contactEconomy == nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "escrow status unavailable"})
		return
	}
	req, ok := h.contactEconomy.FindByEscrowID(id)
	if !ok {
		jsonResponse(w, http.StatusNotFound, map[string]string{"error": "escrow not found"})
		return
	}
	st := protocol.StatusFromContactRequest(req)
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"escrow": st,
		"settleIntent": func() interface{} {
			if req.SettleOutcome == "" || req.SettleTxHash != "" {
				return nil
			}
			return protocol.ContactSettleFromRequest(req, "")
		}(),
	})
}

// AckEscrowSettled POST /api/escrow/settled
// Client reports that escrow_settle was submitted/confirmed; Gateway records settleTxHash.
func (h *Handler) AckEscrowSettled(w http.ResponseWriter, r *http.Request) {
	if h.contactEconomy == nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "unavailable"})
		return
	}
	var body struct {
		EscrowID     string `json:"escrowId"`
		RequestID    string `json:"requestId"`
		SettleTxHash string `json:"settleTxHash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if strings.TrimSpace(body.SettleTxHash) == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "settleTxHash required"})
		return
	}
	requestID := strings.TrimSpace(body.RequestID)
	if requestID == "" && body.EscrowID != "" {
		if req, ok := h.contactEconomy.FindByEscrowID(body.EscrowID); ok {
			requestID = req.RequestID
		}
	}
	if requestID == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "requestId or escrowId required"})
		return
	}
	if err := h.contactEconomy.MarkSettled(requestID, body.SettleTxHash); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"ok":           true,
		"settleTxHash": body.SettleTxHash,
		"txKind":       escrow.TxKindSettle,
	})
}
