package blockchain

import (
	"encoding/json"
	"strconv"
)

// ToCoreJSON returns JSON for Core validate-tx. ok is false if tx is not Core-signed.
func (tx *Transaction) ToCoreJSON() (jsonStr string, ok bool) {
	if tx == nil || tx.SigMain == "" {
		return "", false
	}
	asset := tx.Asset
	if asset == "" {
		asset = "PLP"
	}
	amount := tx.AmountUplp
	if amount == 0 && tx.Value != "" {
		if v, err := strconv.ParseUint(tx.Value, 10, 64); err == nil {
			amount = v
		}
	}
	feeUplp := tx.FeeUplp
	if feeUplp == 0 && tx.Fee != "" {
		if v, err := strconv.ParseUint(tx.Fee, 10, 64); err == nil {
			feeUplp = v
		}
	}
	if feeUplp == 0 {
		feeUplp = 1
	}
	reads := tx.Reads
	if reads == nil {
		reads = []string{}
	}
	writes := tx.Writes
	if writes == nil {
		writes = []string{}
	}
	m := map[string]interface{}{
		"hash":        tx.Hash,
		"from":        tx.From,
		"to":          tx.To,
		"asset":       asset,
		"amount":      amount,
		"fee_uplp":    feeUplp,
		"nonce":       tx.Nonce,
		"reads":       reads,
		"writes":      writes,
		"sig_main":    tx.SigMain,
		"sig_derived": tx.SigDerived,
	}
	if tx.PubMain != "" {
		m["pub_main"] = tx.PubMain
	}
	if tx.PubDerived != "" {
		m["pub_derived"] = tx.PubDerived
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", false
	}
	return string(b), true
}
