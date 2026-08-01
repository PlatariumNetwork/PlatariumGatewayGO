// Package escrow: thin Gateway Escrow API — status queries and settle intents.
// Settlement rules and balances live in PlatariumCore Escrow Engine.
// Gateway does not decide splits; it routes protocol outcomes to Core outcome keys.
package escrow

const (
	TxKindLock   = "escrow_lock"
	TxKindSettle = "escrow_settle"
	TxKindRefund = "escrow_refund"
	TxKindCancel = "escrow_cancel"

	PurposeContact = "contact"

	OutcomeAccept  = "accept"
	OutcomeTimeout = "timeout"
	OutcomeReject  = "reject"
	OutcomeCancel  = "cancel"
)

// Status is a queryable escrow view for clients (financial only).
type Status struct {
	EscrowID     string `json:"escrowId"`
	Purpose      string `json:"purpose,omitempty"`
	Creator      string `json:"creator,omitempty"`
	Beneficiary  string `json:"beneficiary,omitempty"`
	AmountUplp   uint64 `json:"amountUplp,omitempty"`
	Asset        string `json:"asset,omitempty"`
	Status       string `json:"status"`
	LockTxHash   string `json:"lockTxHash,omitempty"`
	SettleTxHash string `json:"settleTxHash,omitempty"`
	ExpiresAt    int64  `json:"expiresAt,omitempty"`
	OutcomeKey   string `json:"outcomeKey,omitempty"`
}

// SettleIntent is what clients must sign/submit as escrow_settle to Core.
// Gateway never applies these amounts itself.
type SettleIntent struct {
	TxKind           string `json:"txKind"`
	EscrowID         string `json:"escrowId"`
	Purpose          string `json:"purpose"`
	OutcomeKey       string `json:"outcomeKey"`
	AmountUplp       uint64 `json:"amountUplp"`
	Beneficiary      string `json:"beneficiary,omitempty"`
	Creator          string `json:"creator,omitempty"`
	SuggestedNode    string `json:"suggestedNode,omitempty"`
	LockTxHash       string `json:"lockTxHash,omitempty"`
}

// LockPayload is the unsigned escrow_lock body for wallet signing.
type LockPayload struct {
	TxKind      string `json:"txKind"`
	EscrowID    string `json:"escrowId"`
	Creator     string `json:"creator"`
	Beneficiary string `json:"beneficiary"`
	Amount      uint64 `json:"amount"`
	Purpose     string `json:"purpose"`
	RulesHash   string `json:"rulesHash,omitempty"`
	ExpiresAt   int64  `json:"expiresAt"`
	Nonce       uint64 `json:"nonce"`
}

// MapProtocolOutcome converts contact-protocol outcome strings to Core rules keys.
func MapProtocolOutcome(protocolOutcome string) string {
	switch protocolOutcome {
	case "accepted", OutcomeAccept:
		return OutcomeAccept
	case "timeout":
		return OutcomeTimeout
	case "rejected", OutcomeReject:
		return OutcomeReject
	case "cancelled", "canceled", OutcomeCancel:
		return OutcomeCancel
	default:
		return ""
	}
}

// IsEscrowTxKind reports whether type is a generic or legacy escrow kind.
func IsEscrowTxKind(t string) bool {
	switch t {
	case TxKindLock, TxKindSettle, TxKindRefund, TxKindCancel,
		"contact_escrow_lock", "contact_escrow_settle":
		return true
	default:
		return false
	}
}
