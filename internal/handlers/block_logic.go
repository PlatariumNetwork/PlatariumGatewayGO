package handlers

import (
	"fmt"
	"time"

	"platarium-gateway-go/internal/blockchain"
	"platarium-gateway-go/internal/core"
	"platarium-gateway-go/internal/logger"
)

func (h *Handler) selectTxsForBlockCollect() []*blockchain.Transaction {
	if h.rustCore == nil || h.blockchain.Ledger() == nil {
		logger.Warn("Core unavailable: block transaction selection stopped")
		return nil
	}

	snap, err := h.blockchain.MempoolSnapshotJSON()
	if err != nil {
		logger.Warn("core select_block_txs snapshot failed: %v", err)
		return nil
	}
	res, err := h.rustCore.SelectBlockTxs(h.blockchain.Ledger().StateFilePath(), snap)
	if err != nil {
		logger.Warn("core select_block_txs failed: %v", err)
		return nil
	}
	txs := h.blockchain.SelectTxsByHashes(res.Hashes)
	if core.DagOrderingEnabled() && len(txs) >= 2 {
		skip := false
		for _, tx := range txs {
			if tx != nil && tx.From == blockchain.FaucetAddress {
				skip = true
				break
			}
		}
		if !skip {
			digests := make([]string, 0, len(txs))
			for _, tx := range txs {
				if tx != nil && tx.Hash != "" {
					digests = append(digests, tx.Hash)
				}
			}
			producer := h.nodesManager.GetNodeID()
			if producer == "" {
				producer = "n0"
			}
			ordered, err := h.rustCore.DagOrderDigests(producer, digests)
			if err != nil {
				logger.Warn("dag_order_digests failed (keeping select order): %v", err)
			} else if ordered != nil && ordered.OK && len(ordered.Digests) > 0 {
				txs = core.PermuteByDigests(ordered.Digests, txs, func(tx *blockchain.Transaction) string {
					if tx == nil {
						return ""
					}
					return tx.Hash
				})
			}
		}
	}
	h.maybeProposeAndBroadcastDag(txs)
	return txs
}

// maybeProposeAndBroadcastDag publishes a Narwhal vertex under shared genesis (P2P on by default).
func (h *Handler) maybeProposeAndBroadcastDag(txs []*blockchain.Transaction) {
	if !core.DagP2PEnabled() || h.rustCore == nil || len(txs) == 0 {
		return
	}
	digests := make([]string, 0, len(txs))
	for _, tx := range txs {
		if tx != nil && tx.Hash != "" && tx.From != blockchain.FaucetAddress {
			digests = append(digests, tx.Hash)
		}
	}
	if len(digests) == 0 {
		return
	}
	author := h.nodesManager.GetNodeID()
	if author == "" {
		author = "n0"
	}
	gen, err := h.rustCore.DagEnsureGenesis()
	if err != nil {
		logger.Warn("dag_ensure_genesis: %v", err)
		return
	}
	parents := []string{gen.Vertex.ID}
	prop, err := h.rustCore.DagPropose(1, author, parents, digests)
	if err != nil {
		logger.Warn("dag_propose batch: %v", err)
		return
	}
	if prop.Status == "rejected" {
		logger.Warn("dag_propose rejected: %s", prop.Error)
		return
	}
	myId := h.nodesManager.GetNodeID()
	go h.nodesManager.BroadcastBlockchainEvent("dag:vertex", core.VertexToMap(gen.Vertex), myId)
	go h.nodesManager.BroadcastBlockchainEvent("dag:vertex", core.VertexToMap(prop.Vertex), myId)
	h.maybeTryDagBatchCommit()
}

func (h *Handler) maybeTryDagBatchCommit() {
	if !core.DagP2PEnabled() || h.rustCore == nil {
		return
	}
	self := h.nodesManager.GetNodeID()
	peers := h.nodesManager.GetConnectedNodes()
	peerIDs := make([]string, 0, len(peers))
	for _, p := range peers {
		peerIDs = append(peerIDs, p.NodeID)
	}
	committee := core.BuildDagCommittee(self, peerIDs)
	if len(committee) == 0 {
		return
	}
	res, err := h.rustCore.DagTryCommitBatches(1, committee, nil)
	if err != nil {
		logger.Warn("dag_try_commit_batches: %v", err)
		return
	}
	if res != nil && res.Committed {
		h.applyDagCommit(res.Anchor, res.Digests)
		logger.Info("dag batch commit digests=%d anchor=%s", len(res.Digests), shortId(res.Anchor))
	}
}

func (h *Handler) applyDagCommit(anchor string, digests []string) {
	if h.blockchain == nil || len(digests) == 0 {
		return
	}
	h.blockchain.SetDagCommitDigests(anchor, digests)
	myId := h.nodesManager.GetNodeID()
	go h.nodesManager.BroadcastBlockchainEvent("dag:commit", map[string]interface{}{
		"anchor":  anchor,
		"digests": digests,
	}, myId)
}

func (h *Handler) onDagCommit(payload map[string]interface{}) {
	if !core.DagP2PEnabled() || h.blockchain == nil {
		return
	}
	anchor, _ := payload["anchor"].(string)
	digests := core.StringSliceFromAny(payload["digests"])
	if len(digests) == 0 {
		return
	}
	h.blockchain.SetDagCommitDigests(anchor, digests)
	logger.Info("dag:commit cached digests=%d anchor=%s", len(digests), shortId(anchor))
}

func (h *Handler) coreBlockProposalStatus() (*core.BlockProposalStatusResult, error) {
	if h.rustCore == nil {
		return nil, fmt.Errorf("rust core unavailable")
	}
	snap, err := h.blockchain.MempoolSnapshotJSON()
	if err != nil {
		return nil, err
	}
	status, err := h.rustCore.BlockProposalStatus(snap, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	return status, nil
}
