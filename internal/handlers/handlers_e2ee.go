package handlers

import (
	"net/http"
	"strings"
)

// GetE2eePubkey GET /api/e2ee-pubkey?address=Px… — last messenger X25519 key registered on this node.
func (h *Handler) GetE2eePubkey(w http.ResponseWriter, r *http.Request) {
	addr := strings.TrimSpace(r.URL.Query().Get("address"))
	if addr == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "address query parameter is required"})
		return
	}
	if h.wsServer == nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "websocket server unavailable"})
		return
	}
	pk := h.wsServer.LookupE2eePubKey(addr)
	if pk == "" {
		jsonResponse(w, http.StatusNotFound, map[string]string{"error": "public key not found for address"})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{
		"address":   addr,
		"publicKey": pk,
	})
}
