// Package protocol: thin protocol-application layer above Escrow API.
// Gateway routes encrypted messages and protocol requests here;
// financial settlement is delegated to Escrow / Core.
package protocol

import (
	"platarium-gateway-go/internal/contacteconomy"
	"platarium-gateway-go/internal/escrow"
)

const PurposeContact = escrow.PurposeContact

// ContactSettleFromRequest builds an escrow_settle intent after a contact respond/timeout.
func ContactSettleFromRequest(req contacteconomy.ContactRequest, nodeHint string) escrow.SettleIntent {
	outcome := req.SettleOutcome
	if outcome == "" {
		outcome = contacteconomy.OutcomeTimeout
	}
	escrowID := req.RequestIDHash
	if escrowID == "" {
		escrowID = contacteconomy.RequestIDHash(req.RequestID)
	}
	return escrow.BuildSettleIntent(
		escrowID,
		PurposeContact,
		outcome,
		req.AmountUplp,
		req.Sender,
		req.Receiver,
		req.LockTxHash,
		nodeHint,
	)
}

// ContactLockPayload builds unsigned escrow_lock for first-contact.
func ContactLockPayload(escrowID, creator, beneficiary string, amount uint64, expiresAt int64, nonce uint64) escrow.LockPayload {
	return escrow.BuildLockPayload(escrowID, creator, beneficiary, PurposeContact, "", amount, expiresAt, nonce)
}

// StatusFromContactRequest projects protocol request metadata into Escrow status DTO.
// Does not invent Core balances — reflects Gateway-known lock/settle hashes and lifecycle.
func StatusFromContactRequest(req contacteconomy.ContactRequest) escrow.Status {
	st := "LOCKED"
	switch req.Status {
	case contacteconomy.StatusEstablished:
		st = "RELEASED"
	case contacteconomy.StatusRejected:
		st = "REFUNDED"
	case contacteconomy.StatusExpired:
		st = "EXPIRED"
	case contacteconomy.StatusPending:
		st = "LOCKED"
	}
	if req.SettleTxHash != "" {
		// settled on chain (client ack)
	}
	return escrow.Status{
		EscrowID:     req.RequestIDHash,
		Purpose:      PurposeContact,
		Creator:      req.Sender,
		Beneficiary:  req.Receiver,
		AmountUplp:   req.AmountUplp,
		Asset:        "PLP",
		Status:       st,
		LockTxHash:   req.LockTxHash,
		SettleTxHash: req.SettleTxHash,
		ExpiresAt:    req.ExpiresAt,
		OutcomeKey:   escrow.MapProtocolOutcome(req.SettleOutcome),
	}
}
