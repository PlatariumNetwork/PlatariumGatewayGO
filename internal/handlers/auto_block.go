package handlers

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"platarium-gateway-go/internal/logger"
)

type autoBlockResponseWriter struct {
	status int
	body   []byte
}

func (w *autoBlockResponseWriter) Header() http.Header { return http.Header{} }

func (w *autoBlockResponseWriter) Write(p []byte) (int, error) {
	w.body = append(w.body, p...)
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return len(p), nil
}

func (w *autoBlockResponseWriter) WriteHeader(statusCode int) { w.status = statusCode }

// AutoBlockEnabled reports whether the background block worker should run.
func AutoBlockEnabled(testnet bool) bool {
	v := strings.TrimSpace(os.Getenv("PLATARIUM_AUTO_BLOCK"))
	if v == "" {
		return testnet
	}
	switch strings.ToLower(v) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func autoBlockDrainMaxRounds() int {
	if v := strings.TrimSpace(os.Getenv("PLATARIUM_AUTO_BLOCK_DRAIN_MAX")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 12
}

func (h *Handler) pruneMempoolBeforeL1() int {
	removed := h.blockchain.PruneMempool()
	if removed > 0 {
		logger.Info("Mempool pruned: removed=%d remaining=%d", removed, len(h.blockchain.GetMempool()))
	}
	return removed
}

// StartAutoBlockWorker runs L1/L2 orchestration; block consensus rules always come from Core.
func (h *Handler) StartAutoBlockWorker() {
	const pollInterval = 500 * time.Millisecond
	logger.Info("Block worker started (poll=%v); proposal/packing/admission via PlatariumCore", pollInterval)
	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for range ticker.C {
			h.autoBlockTick()
		}
	}()
}

func (h *Handler) autoBlockTick() {
	if !h.autoBlockMu.TryLock() {
		return
	}
	defer h.autoBlockMu.Unlock()

	maxRounds := autoBlockDrainMaxRounds()
	for round := 0; round < maxRounds; round++ {
		if len(h.blockchain.GetPendingBlock()) > 0 {
			if !h.autoBlockConfirmPending() {
				return
			}
			continue
		}

		status, err := h.coreBlockProposalStatus()
		if err != nil {
			logger.Warn("Core block proposal status failed: %v", err)
			return
		}
		if !status.ShouldPropose {
			return
		}

		logger.Info("Auto block drain round=%d mempool=%d gas=%d cap=%d",
			round+1, status.MempoolCount, status.MempoolGasUplp, status.BlockGasCapUplp)

		if !h.autoBlockCollectL1() {
			return
		}
		// L1 succeeded → immediately L2 in next loop iteration (no 500ms wait).
	}
}

func (h *Handler) autoBlockConfirmPending() bool {
	pending := h.blockchain.GetPendingBlock()
	if len(pending) == 0 {
		return true
	}
	logger.Info("Auto block: L2 confirm pending=%d", len(pending))

	if outcome := h.validateTxsForL1(pending); !outcome.OK {
		if len(outcome.InvalidHashes) > 0 {
			returned, dropped := h.blockchain.AbandonPendingBlock(outcome.InvalidHashes)
			logger.Warn("Auto L2: abandoned pending returned=%d dropped=%d (%v)",
				returned, dropped, outcome.Err)
			return false
		}
	}

	w := &autoBlockResponseWriter{}
	h.L2ConfirmBlock(w, autoBlockPOST())
	if w.status >= 400 && w.status != 0 {
		logger.Warn("Auto L2 confirm finished with HTTP %d body=%s", w.status, string(w.body))
		if len(h.blockchain.GetPendingBlock()) > 0 {
			returned, dropped := h.blockchain.AbandonPendingBlock(nil)
			logger.Warn("Auto L2 recovery: returned=%d dropped=%d", returned, dropped)
		}
		return false
	}
	return len(h.blockchain.GetPendingBlock()) == 0
}

func (h *Handler) autoBlockCollectL1() bool {
	w := &autoBlockResponseWriter{}
	h.l1CollectBlockRun(w, autoBlockPOST())
	if w.status >= 400 && w.status != 0 {
		logger.Warn("Auto L1 collect finished with HTTP %d body=%s", w.status, string(w.body))
		return false
	}
	return len(h.blockchain.GetPendingBlock()) > 0
}

func autoBlockPOST() *http.Request {
	return &http.Request{Method: http.MethodPost, Header: make(http.Header)}
}
