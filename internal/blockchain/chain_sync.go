package blockchain

import "encoding/json"

// HeadBlockNumber returns the latest confirmed block number, or -1 if empty.
func (bc *Blockchain) HeadBlockNumber() int64 {
	bc.mu.RLock()
	memHead := int64(-1)
	if len(bc.blockHistory) > 0 {
		memHead = bc.blockHistory[len(bc.blockHistory)-1].BlockNumber
	}
	bc.mu.RUnlock()
	if rocksHead, ok := bc.headBlockNumberFromRocks(); ok {
		if rocksHead > memHead {
			return rocksHead
		}
	}
	return memHead
}

// HeadBlock returns a copy of the latest block record, or nil if empty.
func (bc *Blockchain) HeadBlock() *BlockRecord {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if len(bc.blockHistory) == 0 {
		return nil
	}
	out := bc.blockHistory[len(bc.blockHistory)-1]
	return &out
}

// ExportBlocksForSync returns block_confirmed-style payloads for blocks with number >= fromBlock.
func (bc *Blockchain) ExportBlocksForSync(fromBlock int64) []map[string]interface{} {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	out := make([]map[string]interface{}, 0)
	for _, block := range bc.blockHistory {
		if block.BlockNumber < fromBlock {
			continue
		}
		txMaps := make([]interface{}, 0, len(block.TxHashes))
		txHashStrs := make([]interface{}, 0, len(block.TxHashes))
		for _, hash := range block.TxHashes {
			txHashStrs = append(txHashStrs, hash)
			if tx := bc.transactions[hash]; tx != nil {
				txMaps = append(txMaps, transactionToMap(tx))
			}
		}
		entry := map[string]interface{}{
			"blockNumber": block.BlockNumber,
			"timestamp":   block.Timestamp,
			"txHashes":    txHashStrs,
			"txCount":     block.TxCount,
			"totalFees":   block.TotalFees,
			"transactions": txMaps,
			"blockHash":   block.BlockHash,
			"merkleRoot":  block.MerkleRoot,
			"stateRoot":   block.StateRoot,
			"previousHash": block.PreviousHash,
			"producerNodeId": block.ProducerNodeID,
			"l1Yes":       block.L1Yes,
			"l1No":        block.L1No,
			"l2Yes":       block.L2Yes,
			"l2No":        block.L2No,
			"durationMs":  block.DurationMs,
			"l1BeneficiaryNodeId": block.L1BeneficiaryNodeId,
			"l2ConfirmerNodeId":   block.L2ConfirmerNodeId,
		}
		if len(block.L1Votes) > 0 {
			l1 := make(map[string]interface{}, len(block.L1Votes))
			for k, v := range block.L1Votes {
				l1[k] = v
			}
			entry["l1Votes"] = l1
		}
		if len(block.L2Votes) > 0 {
			l2 := make(map[string]interface{}, len(block.L2Votes))
			for k, v := range block.L2Votes {
				l2[k] = v
			}
			entry["l2Votes"] = l2
		}
		out = append(out, entry)
	}
	return out
}

func transactionToMap(tx *Transaction) map[string]interface{} {
	if tx == nil {
		return nil
	}
	b, err := json.Marshal(tx)
	if err != nil {
		return nil
	}
	var m map[string]interface{}
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}
