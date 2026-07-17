package blockchain

import (
	"encoding/json"
	"strconv"
)

// ToCoreJSON converts a gateway transaction to Core validate/apply JSON format.
func ToCoreJSON(tx *Transaction) (string, bool) {
	if tx == nil {
		return "", false
	}
	if tx.From == FaucetAddress {
		return "", false
	}
	if tx.SigMain == "" {
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
	if tx.Timestamp > 0 {
		m["timestamp"] = tx.Timestamp
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
