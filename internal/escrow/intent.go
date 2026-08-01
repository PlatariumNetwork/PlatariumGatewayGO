package escrow

// BuildSettleIntent constructs a Core-bound settle intent from protocol metadata.
// Percentages are NOT computed here — Core Escrow rules engine applies them.
func BuildSettleIntent(
	escrowID, purpose, protocolOutcome string,
	amountUplp uint64,
	creator, beneficiary, lockTxHash, nodeHint string,
) SettleIntent {
	return SettleIntent{
		TxKind:        TxKindSettle,
		EscrowID:      escrowID,
		Purpose:       purpose,
		OutcomeKey:    MapProtocolOutcome(protocolOutcome),
		AmountUplp:    amountUplp,
		Creator:       creator,
		Beneficiary:   beneficiary,
		SuggestedNode: nodeHint,
		LockTxHash:    lockTxHash,
	}
}

// BuildLockPayload builds an unsigned escrow_lock payload (purpose-tagged).
func BuildLockPayload(
	escrowID, creator, beneficiary, purpose, rulesHash string,
	amount uint64, expiresAt int64, nonce uint64,
) LockPayload {
	if purpose == "" {
		purpose = PurposeContact
	}
	return LockPayload{
		TxKind:      TxKindLock,
		EscrowID:    escrowID,
		Creator:     creator,
		Beneficiary: beneficiary,
		Amount:      amount,
		Purpose:     purpose,
		RulesHash:   rulesHash,
		ExpiresAt:   expiresAt,
		Nonce:       nonce,
	}
}
