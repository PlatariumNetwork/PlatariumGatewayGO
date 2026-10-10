package deals

import (
	"encoding/json"
	"os"
	"strings"
)

// CoreEscrow is one record from the Core state file escrows_json array.
type CoreEscrow struct {
	EscrowID     string `json:"escrow_id"`
	Creator      string `json:"creator"`
	Beneficiary  string `json:"beneficiary"`
	Node         string `json:"node"`
	Amount       uint64 `json:"amount"`
	PeacefulFee  uint64 `json:"peaceful_fee"`
	ReservedFee  uint64 `json:"reserved_fee"`
	Asset        string `json:"asset"`
	Purpose      string `json:"purpose"`
	Status       string `json:"status"`
	ExpiresAt    uint64 `json:"expires_at"`
}

// LoadCoreEscrows reads deal and contact escrows from a Core state file.
func LoadCoreEscrows(path string) ([]CoreEscrow, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file struct {
		EscrowsJSON []string `json:"escrows_json"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	out := make([]CoreEscrow, 0, len(file.EscrowsJSON))
	for _, js := range file.EscrowsJSON {
		var row CoreEscrow
		if err := json.Unmarshal([]byte(js), &row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

// DealsForAddress returns purpose=deal rows where address is creator or beneficiary.
func DealsForAddress(rows []CoreEscrow, address string) []CoreEscrow {
	address = strings.TrimSpace(address)
	out := make([]CoreEscrow, 0)
	for _, row := range rows {
		if row.Purpose != "deal" {
			continue
		}
		if strings.EqualFold(row.Creator, address) || strings.EqualFold(row.Beneficiary, address) {
			out = append(out, row)
		}
	}
	return out
}

// DealByID returns the purpose=deal record or false.
func DealByID(rows []CoreEscrow, id string) (CoreEscrow, bool) {
	for _, row := range rows {
		if row.EscrowID == id && row.Purpose == "deal" {
			return row, true
		}
	}
	return CoreEscrow{}, false
}
