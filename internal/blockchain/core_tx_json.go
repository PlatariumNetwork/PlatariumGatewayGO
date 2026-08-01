package blockchain

// ToCoreJSON returns JSON for Core validate-tx / mempool-admit / apply.
// Delegates to package ToCoreJSON so escrow fields stay in the signed hash.
func (tx *Transaction) ToCoreJSON() (jsonStr string, ok bool) {
	return ToCoreJSON(tx)
}
