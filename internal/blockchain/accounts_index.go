package blockchain

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strconv"
)

// AccountListRow is one entry in the top-accounts index.
type AccountListRow struct {
	Address string `json:"address"`
	Balance string `json:"balance"`
	Nonce   uint64 `json:"nonce"`
	TxCount int    `json:"txCount"`
}

// AccountListResult is a paginated slice of accounts sorted by PLP balance (desc).
type AccountListResult struct {
	Accounts              []AccountListRow `json:"accounts"`
	TotalCount            int              `json:"totalCount"`
	TotalSupply           string           `json:"totalSupply"`
	LargestAccountBalance string           `json:"largestAccountBalance,omitempty"`
	Page                  int              `json:"page"`
	Limit                 int              `json:"limit"`
	TotalPages            int              `json:"totalPages"`
}

type stateFileAccounts struct {
	AssetBalances [][]interface{} `json:"asset_balances"`
	Nonces        [][]interface{} `json:"nonces"`
}

// ListTopAccounts reads Core state balances and returns accounts sorted by PLP balance.
func (bc *Blockchain) ListTopAccounts(stateFile string, page, limit int) (AccountListResult, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}

	balances, nonces, err := loadStateBalances(stateFile)
	if err != nil {
		return AccountListResult{}, err
	}

	bc.mu.RLock()
	txCounts := make(map[string]int, len(bc.addressTxs))
	for addr, txs := range bc.addressTxs {
		if addr != "" {
			txCounts[addr] = len(txs)
		}
	}
	bc.mu.RUnlock()

	type row struct {
		address string
		balance *big.Int
		nonce   uint64
		txCount int
	}
	rows := make([]row, 0, len(balances))
	totalSupply := new(big.Int)
	for addr, bal := range balances {
		if bal.Sign() <= 0 {
			continue
		}
		totalSupply.Add(totalSupply, bal)
		rows = append(rows, row{
			address: addr,
			balance: new(big.Int).Set(bal),
			nonce:   nonces[addr],
			txCount: txCounts[addr],
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].balance.Cmp(rows[j].balance) > 0
	})

	totalCount := len(rows)
	totalPages := (totalCount + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * limit
	if start > totalCount {
		start = totalCount
	}
	end := start + limit
	if end > totalCount {
		end = totalCount
	}

	out := make([]AccountListRow, 0, end-start)
	for _, r := range rows[start:end] {
		out = append(out, AccountListRow{
			Address: r.address,
			Balance: r.balance.String(),
			Nonce:   r.nonce,
			TxCount: r.txCount,
		})
	}

	result := AccountListResult{
		Accounts:    out,
		TotalCount:  totalCount,
		TotalSupply: totalSupply.String(),
		Page:        page,
		Limit:       limit,
		TotalPages:  totalPages,
	}
	if len(rows) > 0 {
		result.LargestAccountBalance = rows[0].balance.String()
	}
	return result, nil
}

func loadStateBalances(stateFile string) (map[string]*big.Int, map[string]uint64, error) {
	raw, err := os.ReadFile(stateFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read state file: %w", err)
	}
	var data stateFileAccounts
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, nil, fmt.Errorf("parse state file: %w", err)
	}

	balances := make(map[string]*big.Int)
	for _, entry := range data.AssetBalances {
		if len(entry) < 3 {
			continue
		}
		addr, _ := entry[0].(string)
		asset, _ := entry[1].(string)
		balStr := fmt.Sprint(entry[2])
		if addr == "" || asset != "PLP" {
			continue
		}
		bal, ok := new(big.Int).SetString(balStr, 10)
		if !ok {
			continue
		}
		if existing, ok := balances[addr]; ok {
			existing.Add(existing, bal)
		} else {
			balances[addr] = bal
		}
	}

	nonces := make(map[string]uint64)
	for _, entry := range data.Nonces {
		if len(entry) < 2 {
			continue
		}
		addr, _ := entry[0].(string)
		if addr == "" {
			continue
		}
		switch v := entry[1].(type) {
		case float64:
			nonces[addr] = uint64(v)
		case string:
			if n, err := strconv.ParseUint(v, 10, 64); err == nil {
				nonces[addr] = n
			}
		}
	}

	return balances, nonces, nil
}
