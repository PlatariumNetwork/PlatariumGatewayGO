package handlers

import (
	"os"
	"strings"

	"github.com/gorilla/mux"
)

// ADR flag matrix (fail-closed defaults) — Gateway security / lab controls:
//
//	ENABLE_LAB_ENDPOINTS              default unset/false — lab mutation routes are NOT registered
//	PLATARIUM_CONSENSUS_INSECURE      default false — L1/L2 routes require consensus token
//	PLATARIUM_ALLOW_DEGRADED_CONSENSUS default false — no proposer-only accept when peers silent
//
// Set ENABLE_LAB_ENDPOINTS=1|true|yes to register lab-only mutation routes
// (/api/test-set-load, /api/reward-credit-l1).

// LabEndpointsEnabled reports whether lab mutation routes may be registered.
func LabEndpointsEnabled() bool {
	v := strings.TrimSpace(os.Getenv("ENABLE_LAB_ENDPOINTS"))
	return strings.EqualFold(v, "1") || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// RegisterLabRoutes registers lab mutation endpoints when ENABLE_LAB_ENDPOINTS is set.
// Default (unset/false): no routes registered → mux returns 404.
func RegisterLabRoutes(router *mux.Router, handler *Handler) {
	if handler == nil || router == nil || !LabEndpointsEnabled() {
		return
	}
	router.HandleFunc("/api/test-set-load", handler.TestSetLoad).Methods("POST")
	router.HandleFunc("/api/reward-credit-l1", handler.RewardCreditL1).Methods("POST")
}
