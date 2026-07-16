package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
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

func initPublicChannelPostStore() (*publicchannel.PostStore, error) {
	path := strings.TrimSpace(os.Getenv("PLATARIUM_PUBLIC_CHANNEL_POSTS_FILE"))
	if path == "" {
		path = "data/public-channel-posts.json"
	}
	return publicchannel.NewPostStore(path)
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
	if owner := strings.TrimSpace(r.URL.Query().Get("owner")); owner != "" {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"channels": h.publicChannels.ListByOwner(owner)})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"channels": h.publicChannels.List()})
}

// AppendPublicChannelPost POST /api/public-channels/{address}/posts
// Body: { "from": "Px…", "text": "…", "timestamp": 1234567890 }
func (h *Handler) AppendPublicChannelPost(w http.ResponseWriter, r *http.Request) {
	if h.publicChannels == nil || h.publicChannelPosts == nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "public channel feed unavailable"})
		return
	}
	address := strings.TrimSpace(mux.Vars(r)["address"])
	if address == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "address required"})
		return
	}
	if _, ok := h.publicChannels.Get(address); !ok {
		jsonResponse(w, http.StatusNotFound, map[string]string{"error": "channel not registered"})
		return
	}
	var body struct {
		From      string `json:"from"`
		Text      string `json:"text"`
		Timestamp int64  `json:"timestamp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	post, err := h.publicChannelPosts.Append(address, body.From, body.Text, body.Timestamp)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"post": post})
}

// ListPublicChannelPosts GET /api/public-channels/{address}/posts?since=0&limit=500
func (h *Handler) ListPublicChannelPosts(w http.ResponseWriter, r *http.Request) {
	if h.publicChannels == nil || h.publicChannelPosts == nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "public channel feed unavailable"})
		return
	}
	address := strings.TrimSpace(mux.Vars(r)["address"])
	if address == "" {
		jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "address required"})
		return
	}
	if _, ok := h.publicChannels.Get(address); !ok {
		jsonResponse(w, http.StatusNotFound, map[string]interface{}{"posts": []publicchannel.Post{}})
		return
	}
	since := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
			since = v
		}
	}
	limit := 500
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	posts := h.publicChannelPosts.List(address, since, limit)
	jsonResponse(w, http.StatusOK, map[string]interface{}{"posts": posts})
}

func ensurePublicChannelRegistry(h *Handler) {
	reg, err := initPublicChannelRegistry()
	if err != nil {
		log.Printf("[WARN] public channel registry: %v", err)
		return
	}
	h.publicChannels = reg
	log.Printf("[INFO] Public channel registry ready")

	posts, err := initPublicChannelPostStore()
	if err != nil {
		log.Printf("[WARN] public channel post store: %v", err)
		return
	}
	h.publicChannelPosts = posts
	log.Printf("[INFO] Public channel post store ready")
}
