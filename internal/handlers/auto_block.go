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
}

func (w *autoBlockResponseWriter) Header() http.Header { return http.Header{} }

func (w *autoBlockResponseWriter) Write(p []byte) (int, error) { return len(p), nil }

func (w *autoBlockResponseWriter) WriteHeader(statusCode int) { w.status = statusCode }

// AutoBlockEnabled reports whether the background L1/L2 worker should run.
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

// AutoBlockIntervals returns L1 and L2 ticker intervals from env (defaults 5s / 30s).
func AutoBlockIntervals() (l1 time.Duration, l2 time.Duration) {
	l1Sec := envPositiveInt("PLATARIUM_AUTO_BLOCK_L1_INTERVAL_SEC", 5)
	l2Sec := envPositiveInt("PLATARIUM_AUTO_BLOCK_L2_INTERVAL_SEC", 30)
	if l2Sec < l1Sec {
		l2Sec = l1Sec
	}
	return time.Duration(l1Sec) * time.Second, time.Duration(l2Sec) * time.Second
}

func envPositiveInt(name string, defaultVal int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return defaultVal
	}
	return n
}

// StartAutoBlockWorker runs periodic L1 collect (mempool → pending) and L2 confirm (pending → chain).
func (h *Handler) StartAutoBlockWorker(l1Every, l2Every time.Duration) {
	logger.Info("Auto block worker started: L1 every %v, L2 every %v", l1Every, l2Every)
	go func() {
		l1Ticker := time.NewTicker(l1Every)
		l2Ticker := time.NewTicker(l2Every)
		defer l1Ticker.Stop()
		defer l2Ticker.Stop()
		for {
			select {
			case <-l1Ticker.C:
				h.autoL1Tick()
			case <-l2Ticker.C:
				h.autoL2Tick()
			}
		}
	}()
}

func (h *Handler) autoL1Tick() {
	if len(h.blockchain.GetMempool()) == 0 {
		return
	}
	if len(h.blockchain.GetPendingBlock()) > 0 {
		return
	}
	if !h.autoBlockMu.TryLock() {
		return
	}
	defer h.autoBlockMu.Unlock()

	mempool := len(h.blockchain.GetMempool())
	logger.Info("Auto L1 tick: mempool=%d", mempool)
	w := &autoBlockResponseWriter{}
	h.L1CollectBlock(w, autoBlockPOST())
	if w.status >= 400 && w.status != 0 {
		logger.Warn("Auto L1 collect finished with HTTP %d", w.status)
	}
}

func (h *Handler) autoL2Tick() {
	if len(h.blockchain.GetPendingBlock()) == 0 {
		return
	}
	if !h.autoBlockMu.TryLock() {
		return
	}
	defer h.autoBlockMu.Unlock()

	pending := len(h.blockchain.GetPendingBlock())
	logger.Info("Auto L2 tick: pending=%d", pending)
	w := &autoBlockResponseWriter{}
	h.L2ConfirmBlock(w, autoBlockPOST())
	if w.status >= 400 && w.status != 0 {
		logger.Warn("Auto L2 confirm finished with HTTP %d", w.status)
	}
}

func autoBlockPOST() *http.Request {
	return &http.Request{Method: http.MethodPost, Header: make(http.Header)}
}
