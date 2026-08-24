package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"platarium-gateway-go/internal/channelidentity"
)

func ensureChannelIdentity(h *Handler) {
	path := strings.TrimSpace(os.Getenv("PLATARIUM_CHANNEL_IDENTITY_FILE"))
	if path == "" {
		path = "data/channel-identity.json"
	}
	store, err := channelidentity.NewStore(path)
	if err != nil {
		log.Printf("[WARN] channel identity store: %v", err)
		return
	}
	h.channelIdentity = store
	log.Printf("[INFO] Channel identity registry ready (%s)", path)
}

// GetChannelIdentity GET /api/channel-identity?address=Px…
func (h *Handler) GetChannelIdentity(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if h.channelIdentity == nil {
		http.Error(w, `{"error":"channel identity unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	addr := strings.TrimSpace(r.URL.Query().Get("address"))
	rec, ok := h.channelIdentity.Get(addr)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"channel": nil})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"channel": rec})
}

// PutChannelIdentity POST /api/channel-identity
func (h *Handler) PutChannelIdentity(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if h.channelIdentity == nil {
		http.Error(w, `{"error":"channel identity unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	var body channelidentity.Record
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	wallet := channelidentity.CanonPx(r.Header.Get("X-Platarium-Wallet"))
	owner := channelidentity.CanonPx(body.OwnerAddress)
	addr := channelidentity.CanonPx(body.Address)
	if wallet == "" || owner == "" || addr == "" || wallet != owner {
		http.Error(w, `{"error":"owner wallet header required"}`, http.StatusForbidden)
		return
	}
	body.Address = addr
	body.OwnerAddress = owner
	saved := h.channelIdentity.Put(body)
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"channel": saved})
}
