package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"platarium-gateway-go/internal/blockchain"
	"platarium-gateway-go/internal/core"
	"platarium-gateway-go/internal/logger"
	"platarium-gateway-go/internal/rating"
)

// computeVoteBlockID returns a deterministic proposal ID from chain inputs (no time-only IDs).
func computeVoteBlockID(phase string, blockNumber int64, txHashes []string, stateRoot string) string {
	sorted := append([]string(nil), txHashes...)
	sort.Strings(sorted)
	payload := fmt.Sprintf("%s:%d:%s:%s", phase, blockNumber, strings.Join(sorted, ","), stateRoot)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func txHashesFromTransactions(txs []*blockchain.Transaction) []string {
	out := make([]string, 0, len(txs))
	for _, tx := range txs {
		if tx != nil && tx.Hash != "" {
			out = append(out, tx.Hash)
		}
	}
	sort.Strings(out)
	return out
}

func txsToCoreJSONArray(txs []*blockchain.Transaction) (string, bool) {
	items := make([]string, 0, len(txs))
	for _, tx := range txs {
		if tx == nil {
			continue
		}
		if tx.From == blockchain.FaucetAddress {
			continue
		}
		coreJSON, ok := txToCoreJSON(tx)
		if !ok {
			return "", false
		}
		items = append(items, coreJSON)
	}
	if len(items) == 0 {
		return "[]", true
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// l1ValidateOutcome is the result of Core L1 tx checks (may include invalid hashes to drop).
type l1ValidateOutcome struct {
	OK             bool
	Err            error
	InvalidHashes  []string
	ValidTxs       []*blockchain.Transaction
}

// validateTxsForL1 verifies transactions against Core state (L1 confirmation layer).
// On failure, InvalidHashes lists txs that must be removed from the mempool so the
// block worker is not stuck forever on one bad FIFO entry.
func (h *Handler) validateTxsForL1(txs []*blockchain.Transaction) l1ValidateOutcome {
	if len(txs) == 0 {
		return l1ValidateOutcome{Err: fmt.Errorf("empty transaction set")}
	}
	ledger := h.blockchain.Ledger()
	if ledger == nil {
		if h.testnet {
			return l1ValidateOutcome{Err: fmt.Errorf("core ledger unavailable")}
		}
		return l1ValidateOutcome{OK: true, ValidTxs: txs}
	}
	txsJSON, ok := txsToCoreJSONArray(txs)
	if !ok {
		// Drop any non-Core-compatible entries so they cannot block packing.
		bad := make([]string, 0)
		good := make([]*blockchain.Transaction, 0, len(txs))
		for _, tx := range txs {
			if tx == nil {
				continue
			}
			if tx.From == blockchain.FaucetAddress {
				good = append(good, tx)
				continue
			}
			if _, ok := txToCoreJSON(tx); !ok {
				bad = append(bad, tx.Hash)
				continue
			}
			good = append(good, tx)
		}
		if len(bad) > 0 {
			return l1ValidateOutcome{
				Err:           fmt.Errorf("transactions not Core-compatible"),
				InvalidHashes: bad,
				ValidTxs:      good,
			}
		}
		return l1ValidateOutcome{Err: fmt.Errorf("transactions not Core-compatible")}
	}
	if txsJSON == "[]" {
		return l1ValidateOutcome{OK: true, ValidTxs: txs}
	}
	if h.rustCore != nil {
		res, err := h.rustCore.L1VerifyTxs(ledger.StateFilePath(), txsJSON)
		// Even when Valid=false, Core returns per-tx results — use them to drop poison txs.
		if res != nil && len(res.TxResults) > 0 {
			// Only permanently-invalid txs are dropped from mempool. Future nonces
			// ("expected N, got M" with M>N) must stay — they become packable after N confirms.
			invalid := make([]string, 0)
			validSet := make(map[string]bool, len(res.TxResults))
			for _, tr := range res.TxResults {
				if tr.Valid {
					validSet[tr.Hash] = true
					continue
				}
				if tr.Hash == "" {
					continue
				}
				if isPermanentMempoolInvalid(tr.Error) {
					invalid = append(invalid, tr.Hash)
				}
			}
			good := make([]*blockchain.Transaction, 0, len(txs))
			for _, tx := range txs {
				if tx == nil {
					continue
				}
				if tx.From == blockchain.FaucetAddress || validSet[tx.Hash] {
					good = append(good, tx)
				}
			}
			if len(invalid) > 0 || (err != nil && !res.Valid && len(good) == 0) {
				msg := "L1 verification failed"
				if err != nil {
					msg = err.Error()
				} else if res.Error != "" {
					msg = res.Error
				}
				return l1ValidateOutcome{
					Err:           fmt.Errorf("%s", msg),
					InvalidHashes: invalid,
					ValidTxs:      good,
				}
			}
			if res.Valid && len(invalid) == 0 {
				return l1ValidateOutcome{OK: true, ValidTxs: good}
			}
			// Some txs failed but were kept (future nonce). Pack only the valid prefix.
			if len(good) > 0 {
				return l1ValidateOutcome{OK: true, ValidTxs: good}
			}
			return l1ValidateOutcome{
				Err:           fmt.Errorf("%s", firstNonEmpty(res.Error, "L1 verification failed")),
				InvalidHashes: invalid,
				ValidTxs:      good,
			}
		}
		if err != nil {
			// Fallback: parse hash from "L1 verification failed for tx <hash>"
			if hash := extractFailedTxHash(err.Error()); hash != "" {
				return l1ValidateOutcome{
					Err:           err,
					InvalidHashes: []string{hash},
				}
			}
			return l1ValidateOutcome{Err: err}
		}
		return l1ValidateOutcome{OK: true, ValidTxs: txs}
	}
	for _, tx := range txs {
		if tx == nil || tx.From == blockchain.FaucetAddress {
			continue
		}
		coreJSON, ok := txToCoreJSON(tx)
		if !ok {
			return l1ValidateOutcome{
				Err:           fmt.Errorf("transaction %s not Core-compatible", tx.Hash),
				InvalidHashes: []string{tx.Hash},
			}
		}
		if _, err := ledger.ValidateTx(coreJSON); err != nil {
			if !isPermanentMempoolInvalid(err.Error()) {
				// Future nonce / not yet applicable — keep in mempool, stop packing here.
				good := make([]*blockchain.Transaction, 0)
				for _, t := range txs {
					if t == nil || t == tx {
						break
					}
					good = append(good, t)
				}
				if len(good) > 0 {
					return l1ValidateOutcome{OK: true, ValidTxs: good}
				}
				return l1ValidateOutcome{Err: err, ValidTxs: nil}
			}
			return l1ValidateOutcome{
				Err:           err,
				InvalidHashes: []string{tx.Hash},
			}
		}
	}
	return l1ValidateOutcome{OK: true, ValidTxs: txs}
}

// isPermanentMempoolInvalid reports whether a Core validation error means the tx
// should be deleted from the mempool. Future nonces must NOT be deleted.
func isPermanentMempoolInvalid(errMsg string) bool {
	msg := strings.ToLower(errMsg)
	if msg == "" {
		// Unknown failure from Core without detail — treat as permanent to avoid stalls.
		return true
	}
	// "invalid nonce: expected N, got M"
	if strings.Contains(msg, "nonce") {
		var expected, got int
		if _, err := fmt.Sscanf(msg, "invalid nonce: expected %d, got %d", &expected, &got); err == nil {
			if got > expected {
				return false
			}
			return true // stale / duplicate (got < expected or equal mishandled)
		}
		// Loose match for nested error strings.
		reExp := strings.Index(msg, "expected ")
		reGot := strings.Index(msg, "got ")
		if reExp >= 0 && reGot > reExp {
			var expected, got int
			if _, err := fmt.Sscanf(msg[reExp:], "expected %d, got %d", &expected, &got); err == nil && got > expected {
				return false
			}
		}
	}
	return true
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func extractFailedTxHash(msg string) string {
	const prefix = "L1 verification failed for tx "
	idx := strings.Index(msg, prefix)
	if idx < 0 {
		return ""
	}
	hash := strings.TrimSpace(msg[idx+len(prefix):])
	if i := strings.IndexAny(hash, " \t\n\r,;"); i >= 0 {
		hash = hash[:i]
	}
	if len(hash) < 16 {
		return ""
	}
	return hash
}

// extractApplyTxHash pulls a tx hash from L2 apply errors or JSON error bodies.
// Matches: `apply tx <hash>:` and bare hash mentions after "apply tx ".
func extractApplyTxHash(body []byte) string {
	msg := string(body)
	const prefix = "apply tx "
	idx := strings.Index(msg, prefix)
	if idx < 0 {
		if h := extractFailedTxHash(msg); h != "" {
			return h
		}
		return ""
	}
	hash := strings.TrimSpace(msg[idx+len(prefix):])
	if i := strings.IndexAny(hash, " \t\n\r,:;"); i >= 0 {
		hash = hash[:i]
	}
	// Strip JSON trailing quote if present.
	hash = strings.Trim(hash, `"'`)
	if len(hash) < 16 {
		return ""
	}
	return hash
}

func (h *Handler) assembleBlockHeader(blockNumber int64, txs []*blockchain.Transaction, producerNodeID string, timestamp int64) (*core.BlockHeader, error) {
	ledger := h.blockchain.Ledger()
	if ledger == nil || h.rustCore == nil {
		return nil, fmt.Errorf("core ledger unavailable")
	}
	txHashes := txHashesFromTransactions(txs)
	hashesJSON, err := json.Marshal(txHashes)
	if err != nil {
		return nil, err
	}
	prevHash := h.blockchain.PreviousHashForBlock(blockNumber)
	return h.rustCore.AssembleBlock(
		ledger.StateFilePath(),
		uint64(blockNumber),
		prevHash,
		timestamp,
		string(hashesJSON),
		producerNodeID,
	)
}

func stringSliceToInterface(items []string) []interface{} {
	out := make([]interface{}, len(items))
	for i, s := range items {
		out[i] = s
	}
	return out
}

func parseStringSlice(raw interface{}) []string {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (h *Handler) resolveMempoolTxsForProposal(txCount int, txHashes []string) ([]*blockchain.Transaction, bool) {
	if len(txHashes) > 0 {
		txs, ok := h.blockchain.GetMempoolTxsByHashes(txHashes)
		return txs, ok
	}
	mempool := h.blockchain.GetMempool()
	if len(mempool) != txCount {
		return nil, false
	}
	return mempool, true
}

func (h *Handler) submitL1Vote(blockId, proposerNodeId string, txCount int, txHashes []string) {
	myId := h.nodesManager.GetNodeID()
	const maxAttempts = 8
	const retryDelay = 250 * time.Millisecond
	var yes bool
	var resolved bool
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay)
		}
		txs, ok := h.resolveMempoolTxsForProposal(txCount, txHashes)
		if !ok || len(txs) == 0 {
			continue
		}
		outcome := h.validateTxsForL1(txs)
		if !outcome.OK {
			logger.Info("L1 proposal validation failed from %s: %v", shortId(proposerNodeId), outcome.Err)
			yes = false
			resolved = true
			break
		}
		yes = true
		resolved = true
		break
	}
	if !resolved {
		logger.Info("L1 proposal from %s: mempool not ready (txCount=%d hashes=%d)", shortId(proposerNodeId), txCount, len(txHashes))
	}
	logger.Info("L1 received proposal from %s (txCount=%d), sending vote yes=%v", shortId(proposerNodeId), txCount, yes)
	go h.nodesManager.BroadcastBlockchainEvent("l1_vote", map[string]interface{}{
		"blockId": blockId, "nodeId": myId, "yes": yes,
	}, myId)
}

func (h *Handler) submitL2Vote(blockId, proposerNodeId string, txHashes []string) {
	myId := h.nodesManager.GetNodeID()
	const maxAttempts = 8
	const retryDelay = 250 * time.Millisecond
	var yes bool
	var resolved bool
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay)
		}
		pending := h.blockchain.GetPendingBlock()
		if len(txHashes) > 0 && !h.blockchain.PendingBlockMatchesHashes(txHashes) {
			continue
		}
		if len(pending) == 0 {
			continue
		}
		outcome := h.validateTxsForL1(pending)
		if !outcome.OK {
			logger.Info("L2 block validation failed from %s: %v", shortId(proposerNodeId), outcome.Err)
			yes = false
			resolved = true
			break
		}
		yes = true
		resolved = true
		break
	}
	if !resolved {
		logger.Info("L2 proposal from %s: pending block not ready (hashes=%d)", shortId(proposerNodeId), len(txHashes))
	}
	logger.Info("L2 received proposal from %s blockId=%s..., sending vote yes=%v", shortId(proposerNodeId), shortId(blockId), yes)
	go h.nodesManager.BroadcastBlockchainEvent("l2_vote", map[string]interface{}{
		"blockId": blockId, "nodeId": myId, "yes": yes,
	}, myId)
}

func votesToCoreJSON(votes map[string]bool) (string, error) {
	type voteEntry struct {
		NodeID string `json:"node_id"`
		Yes    bool   `json:"yes"`
	}
	entries := make([]voteEntry, 0, len(votes))
	for id, yes := range votes {
		entries = append(entries, voteEntry{NodeID: id, Yes: yes})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].NodeID < entries[j].NodeID })
	b, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func allowDegradedConsensus() bool {
	v := os.Getenv("PLATARIUM_ALLOW_DEGRADED_CONSENSUS")
	if v == "" {
		return true
	}
	return strings.EqualFold(v, "1") || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// finalizeVoteRoundWithCore uses Core vote aggregation when available.
func (h *Handler) finalizeVoteRoundWithCore(votes map[string]bool, isL1 bool, timeoutAccepted bool) (accepted bool, toPenalize []string) {
	accepted = timeoutAccepted
	if h.rustCore == nil || len(votes) == 0 {
		return accepted, nil
	}
	votesJSON, err := votesToCoreJSON(votes)
	if err != nil {
		return accepted, nil
	}
	var res *core.VoteResult
	if isL1 {
		res, err = h.rustCore.L1ProcessVotes(votesJSON)
	} else {
		res, err = h.rustCore.L2ProcessVotes(votesJSON)
	}
	if err != nil || res == nil {
		logger.Warn("Core process-votes failed, using timeout result: %v", err)
		return accepted, nil
	}
	return res.Confirmed, res.ToPenalize
}

func (h *Handler) applyVoteSlashing(votes map[string]bool, toPenalize []string, committee map[string]bool) {
	if len(toPenalize) > 0 {
		h.nodeRegistry.ApplySlashBatch(toPenalize, rating.SlashAgainstMajority)
		for _, id := range toPenalize {
			logger.Info("slash node=%s reason=%s", shortId(id), rating.ReasonName(rating.SlashAgainstMajority))
		}
	}
	if committee == nil {
		return
	}
	var noVote []string
	for id := range committee {
		if _, voted := votes[id]; !voted {
			noVote = append(noVote, id)
		}
	}
	if len(noVote) == 0 {
		return
	}
	h.nodeRegistry.ApplySlashBatch(noVote, rating.SlashNoVote)
	for _, id := range noVote {
		logger.Info("slash node=%s reason=%s", shortId(id), rating.ReasonName(rating.SlashNoVote))
	}
}
