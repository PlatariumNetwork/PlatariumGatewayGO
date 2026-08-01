package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"platarium-gateway-go/internal/blockchain"
	"platarium-gateway-go/internal/core"
	"platarium-gateway-go/internal/escrow"
	"platarium-gateway-go/internal/ratelimit"
)

// SubmitEscrowLock POST /api/escrow/lock
// Signs escrow_lock via Core (mnemonic) and admits to mempool. Real PLP debit on apply.
func (h *Handler) SubmitEscrowLock(w http.ResponseWriter, r *http.Request) {
	if !h.allowContactRate(r, "escrow_lock") {
		jsonResponse(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
		return
	}
	var body struct {
		Mnemonic     string `json:"mnemonic"`
		Alphanumeric string `json:"alphanumeric"`
		From         string `json:"from"`
		Beneficiary  string `json:"beneficiary"`
		EscrowID     string `json:"escrowId"`
		AmountUplp   uint64 `json:"amountUplp"`
		FeeUplp      uint64 `json:"feeUplp"`
		Nonce        *int   `json:"nonce"`
		ExpiresAt    int64  `json:"expiresAt"`
		Purpose      string `json:"purpose"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if h.rustCore == nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "core unavailable"})
		return
	}
	if strings.TrimSpace(body.Mnemonic) == "" || strings.TrimSpace(body.Alphanumeric) == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "mnemonic and alphanumeric required"})
		return
	}
	if body.EscrowID == "" || body.Beneficiary == "" || body.AmountUplp == 0 {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "escrowId, beneficiary, amountUplp required"})
		return
	}
	purpose := body.Purpose
	if purpose == "" {
		purpose = escrow.PurposeContact
	}
	from := strings.TrimSpace(body.From)
	if from == "" {
		keys, err := h.rustCore.GenerateKeys(body.Mnemonic, body.Alphanumeric, 0)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "GenerateKeys: " + err.Error()})
			return
		}
		from = keys["publicKey"]
	}
	fee := body.FeeUplp
	if fee == 0 {
		fee = 1
	}
	nonce := 0
	if body.Nonce != nil {
		nonce = *body.Nonce
	} else {
		n, err := h.allocateNonceForAddress(from)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "nonce: " + err.Error()})
			return
		}
		nonce = n
	}
	expires := body.ExpiresAt
	if expires == 0 {
		expires = time.Now().Unix() + 30*24*3600
	}
	opts := &core.EscrowSignOpts{
		TxKind:    escrow.TxKindLock,
		EscrowID:  body.EscrowID,
		Purpose:   purpose,
		ExpiresAt: uint64(expires),
		SettlePayee: body.Beneficiary,
	}
	signedJSON, err := h.rustCore.SignTransactionExt(
		from, body.Beneficiary, "PLP", body.AmountUplp, fee, uint64(nonce),
		[]string{}, []string{from, body.Beneficiary},
		body.Mnemonic, body.Alphanumeric, opts,
	)
	if err != nil {
		_ = h.releaseNonceForAddress(from, nonce)
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "SignTransaction: " + err.Error()})
		return
	}
	tx, err := parseSignedEscrowTx(signedJSON, escrow.TxKindLock)
	if err != nil {
		_ = h.releaseNonceForAddress(from, nonce)
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.admitToMempool(tx); err != nil {
		_ = h.releaseNonceForAddress(from, nonce)
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"lockTxHash":  tx.Hash,
		"escrowId":    body.EscrowID,
		"transaction": tx,
	})
}

// SubmitEscrowSettle POST /api/escrow/settle
func (h *Handler) SubmitEscrowSettle(w http.ResponseWriter, r *http.Request) {
	if !h.allowContactRate(r, "escrow_settle") {
		jsonResponse(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
		return
	}
	var body struct {
		Mnemonic     string `json:"mnemonic"`
		Alphanumeric string `json:"alphanumeric"`
		From         string `json:"from"`
		EscrowID     string `json:"escrowId"`
		AmountUplp   uint64 `json:"amountUplp"`
		FeeUplp      uint64 `json:"feeUplp"`
		Nonce        *int   `json:"nonce"`
		OutcomeKey   string `json:"outcomeKey"`
		Beneficiary  string `json:"beneficiary"`
		Creator      string `json:"creator"`
		Node         string `json:"node"`
		Purpose      string `json:"purpose"`
		RequestID    string `json:"requestId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if h.rustCore == nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "core unavailable"})
		return
	}
	if strings.TrimSpace(body.Mnemonic) == "" || strings.TrimSpace(body.Alphanumeric) == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "mnemonic and alphanumeric required"})
		return
	}
	outcome := escrow.MapProtocolOutcome(body.OutcomeKey)
	if outcome == "" {
		outcome = strings.TrimSpace(body.OutcomeKey)
	}
	if body.EscrowID == "" || body.AmountUplp == 0 || outcome == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "escrowId, amountUplp, outcomeKey required"})
		return
	}
	from := strings.TrimSpace(body.From)
	if from == "" {
		keys, err := h.rustCore.GenerateKeys(body.Mnemonic, body.Alphanumeric, 0)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "GenerateKeys: " + err.Error()})
			return
		}
		from = keys["publicKey"]
	}
	to := body.Creator
	if to == "" {
		to = body.Beneficiary
	}
	if to == "" {
		to = from
	}
	fee := body.FeeUplp
	if fee == 0 {
		fee = 1
	}
	nonce := 0
	if body.Nonce != nil {
		nonce = *body.Nonce
	} else {
		n, err := h.allocateNonceForAddress(from)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "nonce: " + err.Error()})
			return
		}
		nonce = n
	}
	purpose := body.Purpose
	if purpose == "" {
		purpose = escrow.PurposeContact
	}
	opts := &core.EscrowSignOpts{
		TxKind:           escrow.TxKindSettle,
		EscrowID:         body.EscrowID,
		Purpose:          purpose,
		SettleOutcomeKey: outcome,
		SettlePayee:      body.Beneficiary,
		SettleNode:       body.Node,
	}
	signedJSON, err := h.rustCore.SignTransactionExt(
		from, to, "PLP", body.AmountUplp, fee, uint64(nonce),
		[]string{}, []string{from, to},
		body.Mnemonic, body.Alphanumeric, opts,
	)
	if err != nil {
		_ = h.releaseNonceForAddress(from, nonce)
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "SignTransaction: " + err.Error()})
		return
	}
	tx, err := parseSignedEscrowTx(signedJSON, escrow.TxKindSettle)
	if err != nil {
		_ = h.releaseNonceForAddress(from, nonce)
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.admitToMempool(tx); err != nil {
		_ = h.releaseNonceForAddress(from, nonce)
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if h.contactEconomy != nil && body.RequestID != "" {
		_ = h.contactEconomy.MarkSettled(body.RequestID, tx.Hash)
	} else if h.contactEconomy != nil {
		if req, ok := h.contactEconomy.FindByEscrowID(body.EscrowID); ok {
			_ = h.contactEconomy.MarkSettled(req.RequestID, tx.Hash)
		}
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"settleTxHash": tx.Hash,
		"escrowId":     body.EscrowID,
		"transaction":  tx,
	})
}

func parseSignedEscrowTx(signedJSON, defaultType string) (*blockchain.Transaction, error) {
	var coreTx struct {
		Hash             string   `json:"hash"`
		From             string   `json:"from"`
		To               string   `json:"to"`
		Asset            string   `json:"asset"`
		Amount           uint64   `json:"amount"`
		FeeUplp          uint64   `json:"fee_uplp"`
		Nonce            int      `json:"nonce"`
		SigMain          string   `json:"sig_main"`
		SigDerived       string   `json:"sig_derived"`
		PubMain          string   `json:"pub_main"`
		PubDerived       string   `json:"pub_derived"`
		Reads            []string `json:"reads"`
		Writes           []string `json:"writes"`
		TxKind           string   `json:"tx_kind"`
		EscrowID         string   `json:"escrow_id"`
		RequestIDHash    string   `json:"request_id_hash"`
		Purpose          string   `json:"purpose"`
		ExpiresAt        uint64   `json:"expires_at"`
		SettleOutcomeKey string   `json:"settle_outcome_key"`
		SettlePayee      string   `json:"settle_payee"`
		SettleNode       string   `json:"settle_node"`
	}
	if err := json.Unmarshal([]byte(signedJSON), &coreTx); err != nil {
		return nil, fmt.Errorf("parse signed tx: %w", err)
	}
	kind := coreTx.TxKind
	if kind == "" {
		kind = defaultType
	}
	escrowID := coreTx.EscrowID
	if escrowID == "" {
		escrowID = coreTx.RequestIDHash
	}
	tx := &blockchain.Transaction{
		Hash:             coreTx.Hash,
		From:             coreTx.From,
		To:               coreTx.To,
		Value:            strconv.FormatUint(coreTx.Amount, 10),
		Fee:              strconv.FormatUint(coreTx.FeeUplp, 10),
		Nonce:            coreTx.Nonce,
		Timestamp:        time.Now().Unix(),
		Type:             kind,
		AssetType:        "native",
		SigMain:          coreTx.SigMain,
		SigDerived:       coreTx.SigDerived,
		PubMain:          coreTx.PubMain,
		PubDerived:       coreTx.PubDerived,
		Asset:            coreTx.Asset,
		AmountUplp:       coreTx.Amount,
		FeeUplp:          coreTx.FeeUplp,
		Reads:            coreTx.Reads,
		Writes:           coreTx.Writes,
		EscrowID:         escrowID,
		RequestIDHash:    escrowID,
		Purpose:          coreTx.Purpose,
		ExpiresAt:        int64(coreTx.ExpiresAt),
		SettleOutcomeKey: coreTx.SettleOutcomeKey,
		SettlePayee:      coreTx.SettlePayee,
		SettleNode:       coreTx.SettleNode,
	}
	return tx, nil
}

func (h *Handler) allowContactRate(r *http.Request, action string) bool {
	if h.contactRate == nil {
		return true
	}
	ip := ratelimit.ClientIP(r)
	dev := ratelimit.DeviceID(r)
	key := action + "|" + ip
	if dev != "" {
		key = action + "|dev:" + dev
	}
	return h.contactRate.Allow(key)
}

// allocateNonceForAddress / releaseNonceForAddress — thin wrappers used by escrow submit.
func (h *Handler) allocateNonceForAddress(address string) (int, error) {
	if address == "" {
		return 0, fmt.Errorf("empty address")
	}
	if h.blockchain == nil {
		return 0, fmt.Errorf("blockchain unavailable")
	}
	n, _, err := h.allocateNonceLocked(address)
	return int(n), err
}

func (h *Handler) releaseNonceForAddress(address string, nonce int) error {
	if h.blockchain == nil || nonce < 0 {
		return nil
	}
	h.releaseNonceLocked(address, uint64(nonce))
	return nil
}
