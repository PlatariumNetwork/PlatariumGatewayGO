package handlers

import (
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/mux"
)

// ADR flag matrix (fail-closed defaults) — Gateway security / lab / consensus controls.
//
// Principle: uncertainty never increases authority. Missing Core, missing peers, missing
// tokens, or unset flags must not open mutation or finality paths.
//
//	Flag                              Default     Testnet intent                         Production intent
//	--------------------------------  ----------  -------------------------------------  --------------------------------
//	ENABLE_LAB_ENDPOINTS              unset/off   optional local labs only               must stay off
//	PLATARIUM_LAB_TOKEN               empty       required when lab on                   N/A (lab off)
//	PLATARIUM_CONSENSUS_INSECURE      false       local-only opt-in without token        must stay false
//	PLATARIUM_ALLOW_DEGRADED_CONSENSUS false      opt-in solo/dev proposer-only accept   must stay false
//
// ENABLE_LAB_ENDPOINTS=1|true|yes registers lab mutation routes
// (/api/test-set-load, /api/reward-credit-l1). Even when registered, every request
// requires PLATARIUM_LAB_TOKEN (Bearer or X-Platarium-Lab-Token).

// LabEndpointsEnabled reports whether lab mutation routes may be registered.
func LabEndpointsEnabled() bool {
	v := strings.TrimSpace(os.Getenv("ENABLE_LAB_ENDPOINTS"))
	return strings.EqualFold(v, "1") || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// requireLabAuth gates lab mutation handlers when routes are registered.
// Missing/empty configured token or missing request credential → 401.
// Present but wrong credential → 403.
func requireLabAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		expected := strings.TrimSpace(os.Getenv("PLATARIUM_LAB_TOKEN"))
		if expected == "" {
			http.Error(w, "lab auth required: set PLATARIUM_LAB_TOKEN", http.StatusUnauthorized)
			return
		}
		got := strings.TrimSpace(r.Header.Get("X-Platarium-Lab-Token"))
		if got == "" {
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
				got = strings.TrimSpace(auth[7:])
			}
		}
		if got == "" {
			http.Error(w, "lab auth required", http.StatusUnauthorized)
			return
		}
		if got != expected {
			http.Error(w, "forbidden lab route", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// RegisterLabRoutes registers lab mutation endpoints when ENABLE_LAB_ENDPOINTS is set.
// Default (unset/false): no routes registered → mux returns 404.
// When enabled, handlers are wrapped with requireLabAuth.
func RegisterLabRoutes(router *mux.Router, handler *Handler) {
	if handler == nil || router == nil || !LabEndpointsEnabled() {
		return
	}
	router.HandleFunc("/api/test-set-load", requireLabAuth(handler.TestSetLoad)).Methods("POST")
	router.HandleFunc("/api/reward-credit-l1", requireLabAuth(handler.RewardCreditL1)).Methods("POST")
}
