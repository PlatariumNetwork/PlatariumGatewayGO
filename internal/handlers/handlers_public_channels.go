package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"platarium-gateway-go/internal/publicchannel"

	"github.com/gorilla/mux"
)

func initPublicChannelRegistry() (*publicchannel.Registry, error) {
	path := strings.TrimSpace(os.Getenv("PLATARIUM_PUBLIC_CHANNELS_FILE"))
	if path == "" {
		path = "data/public-channels.json"
	}
	return publicchannel.NewRegistry(path)
}

// RegisterPublicChannel POST /api/public-channels
// Body: { "address": "Px…", "ownerAddress": "Px…", "name": "optional" }
func (h *Handler) RegisterPublicChannel(w http.ResponseWriter, r *http.Request) {
	if h.publicChannels == nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "public channel registry unavailable"})
		return
	}
	var body struct {
		Address      string `json:"address"`
		OwnerAddress string `json:"ownerAddress"`
		Name         string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	rec, err := h.publicChannels.Register(body.Address, body.OwnerAddress, body.Name)
	if err != nil {
		msg := err.Error()
		status := http.StatusBadRequest
		if strings.Contains(msg, "another owner") {
			status = http.StatusConflict
		}
		jsonResponse(w, status, map[string]string{"error": msg})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"channel": rec})
}

// GetPublicChannel GET /api/public-channels/{address}
func (h *Handler) GetPublicChannel(w http.ResponseWriter, r *http.Request) {
	if h.publicChannels == nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "public channel registry unavailable"})
		return
	}
	address := strings.TrimSpace(mux.Vars(r)["address"])
	if address == "" {
		address = strings.TrimSpace(r.URL.Query().Get("address"))
	}
	if address == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "address required"})
		return
	}
	rec, ok := h.publicChannels.Get(address)
	if !ok {
		jsonResponse(w, http.StatusNotFound, map[string]interface{}{"channel": nil})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"channel": rec})
}

// ListPublicChannels GET /api/public-channels
func (h *Handler) ListPublicChannels(w http.ResponseWriter, r *http.Request) {
	if h.publicChannels == nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "public channel registry unavailable"})
		return
	}
	if addr := strings.TrimSpace(r.URL.Query().Get("address")); addr != "" {
		rec, ok := h.publicChannels.Get(addr)
		if !ok {
			jsonResponse(w, http.StatusNotFound, map[string]interface{}{"channel": nil})
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"channel": rec})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"channels": h.publicChannels.List()})
}

func ensurePublicChannelRegistry(h *Handler) {
	reg, err := initPublicChannelRegistry()
	if err != nil {
		log.Printf("[WARN] public channel registry: %v", err)
		return
	}
	h.publicChannels = reg
	log.Printf("[INFO] Public channel registry ready")
}
