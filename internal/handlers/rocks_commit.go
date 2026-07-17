package handlers

import (
	"fmt"

	"platarium-gateway-go/internal/blockchain"
	"platarium-gateway-go/internal/core"
	"platarium-gateway-go/internal/logger"
)

func (h *Handler) commitBlockToRocks(block blockchain.BlockRecord, txs []*blockchain.Transaction, stateRoot string) error {
	rocks := h.blockchain.RocksStore()
	if rocks == nil || !rocks.Enabled() {
		return nil
	}
	if stateRoot == "" {
		if ledger := h.blockchain.Ledger(); ledger != nil {
			if sr, err := ledger.StateRoot(); err == nil {
				stateRoot = sr
			}
		}
	}
	if stateRoot == "" {
		return fmt.Errorf("state root required for rocks commit")
	}

	height := core.GatewayBlockToRocksHeight(block.BlockNumber)
	txHashes := make([]string, 0, len(txs))
	txJSONs := make([]string, 0, len(txs))
	receipts := make([]core.BlockReceipt, 0, len(txs))
	addrSet := make(map[string]struct{})

	for _, tx := range txs {
		if tx == nil {
			continue
		}
		coreJSON, ok := tx.ToCoreJSON()
		if !ok {
			return fmt.Errorf("transaction %s not Core-compatible for rocks commit", tx.Hash)
		}
		txHashes = append(txHashes, tx.Hash)
		txJSONs = append(txJSONs, coreJSON)
		receipts = append(receipts, core.BlockReceipt{
			TxHash:      tx.Hash,
			Status:      "ok",
			FeeUplp:     tx.FeeUplp,
			BlockHeight: height,
		})
		if tx.From != "" {
			addrSet[tx.From] = struct{}{}
		}
		if tx.To != "" {
			addrSet[tx.To] = struct{}{}
		}
	}

	accounts := make([]core.RocksAccount, 0, len(addrSet))
	ledger := h.blockchain.Ledger()
	for addr := range addrSet {
		var acct core.RocksAccount
		if ledger != nil {
			q, err := ledger.Query(addr)
			if err != nil {
				return fmt.Errorf("query account %s: %w", addr, err)
			}
			acct = core.RocksAccount{
				Address:     q.Address,
				Balance:     q.Balance,
				UplpBalance: q.UplpBalance,
				Nonce:       q.Nonce,
			}
		} else {
			found, ra, err := rocks.RocksGetAccount(addr)
			if err != nil {
				return err
			}
			if found && ra != nil {
				acct = *ra
			} else {
				acct = core.RocksAccount{Address: addr, Balance: "0", UplpBalance: "0"}
			}
		}
		accounts = append(accounts, acct)
	}

	commit := &core.BlockCommitPayload{
		Block: core.RocksBlockStored{
			Height:       height,
			PreviousHash: block.PreviousHash,
			Timestamp:    block.Timestamp,
			TxHashes:     txHashes,
			MerkleRoot:   block.MerkleRoot,
			StateRoot:    stateRoot,
			BlockHash:    block.BlockHash,
			ProducerID:   block.ProducerNodeID,
		},
		TxJSONs:   txJSONs,
		Accounts:  accounts,
		Receipts:  receipts,
		StateRoot: stateRoot,
	}

	committedHeight, err := rocks.RocksCommitBlock(commit)
	if err != nil {
		return err
	}
	logger.Info("RocksDB block committed height=%d gatewayBlock=%d txs=%d", committedHeight, block.BlockNumber, len(txs))
	return nil
}
