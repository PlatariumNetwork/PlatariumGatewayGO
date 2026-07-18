package core

import (
	"encoding/json"
	"fmt"
)

// BlockHeader is the parsed assemble-block response from platarium-cli.
type BlockHeader struct {
	BlockNumber       uint64   `json:"block_number"`
	PreviousHash      string   `json:"previous_hash"`
	Timestamp         int64    `json:"timestamp"`
	TransactionHashes []string `json:"transaction_hashes"`
	MerkleRoot        string   `json:"merkle_root"`
	StateRoot         string   `json:"state_root"`
	BlockHash         string   `json:"block_hash"`
	ProducerID        string   `json:"producer_id"`
}

// VoteResult is the parsed l1/l2-process-votes response.
type VoteResult struct {
	Confirmed  bool     `json:"confirmed"`
	ToPenalize []string `json:"to_penalize"`
	Error      string   `json:"error"`
}

// L1TxVerifyResult is one entry from l1-verify-txs tx_results.
type L1TxVerifyResult struct {
	Hash  string `json:"hash"`
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

// L1VerifyResult is the parsed l1-verify-txs response.
type L1VerifyResult struct {
	Valid     bool               `json:"valid"`
	Error     string             `json:"error"`
	TxResults []L1TxVerifyResult `json:"tx_results"`
}

// L1VerifyTxs verifies all transactions against state for L1 confirmation.
func (rc *RustCore) L1VerifyTxs(stateFile, txsJSON string) (*L1VerifyResult, error) {
	output, err := rc.Execute([]string{
		"l1-verify-txs",
		"--state-file", stateFile,
		"--txs", txsJSON,
	})
	if err != nil {
		return nil, err
	}
	var out L1VerifyResult
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		return nil, fmt.Errorf("parse l1-verify-txs: %w", err)
	}
	if !out.Valid {
		if out.Error != "" {
			return &out, fmt.Errorf("%s", out.Error)
		}
		return &out, fmt.Errorf("L1 verification failed")
	}
	return &out, nil
}

// L1ProcessVotes aggregates L1 votes via Core confirmation layer.
func (rc *RustCore) L1ProcessVotes(votesJSON string) (*VoteResult, error) {
	output, err := rc.Execute([]string{"l1-process-votes", "--votes", votesJSON})
	if err != nil {
		return nil, err
	}
	var res VoteResult
	if err := json.Unmarshal([]byte(output), &res); err != nil {
		return nil, fmt.Errorf("parse l1-process-votes: %w", err)
	}
	return &res, nil
}

// L2ProcessVotes aggregates L2 block votes via Core block assembly.
func (rc *RustCore) L2ProcessVotes(votesJSON string) (*VoteResult, error) {
	output, err := rc.Execute([]string{"l2-process-votes", "--votes", votesJSON})
	if err != nil {
		return nil, err
	}
	var res VoteResult
	if err := json.Unmarshal([]byte(output), &res); err != nil {
		return nil, fmt.Errorf("parse l2-process-votes: %w", err)
	}
	return &res, nil
}

// AssembleBlock builds a Core block header from state file and transaction hashes.
func (rc *RustCore) AssembleBlock(stateFile string, blockNumber uint64, previousHash string, timestamp int64, txHashesJSON, producerID string) (*BlockHeader, error) {
	output, err := rc.Execute([]string{
		"assemble-block",
		"--state-file", stateFile,
		"--block-number", fmt.Sprintf("%d", blockNumber),
		"--previous-hash", previousHash,
		"--timestamp", fmt.Sprintf("%d", timestamp),
		"--tx-hashes", txHashesJSON,
		"--producer-id", producerID,
	})
	if err != nil {
		return nil, err
	}
	var header BlockHeader
	if err := json.Unmarshal([]byte(output), &header); err != nil {
		return nil, fmt.Errorf("parse assemble-block: %w", err)
	}
	return &header, nil
}
