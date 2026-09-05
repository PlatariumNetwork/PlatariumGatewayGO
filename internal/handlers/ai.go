package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AiChatCompletions proxies OpenAI-compatible chat to upstream (Phase 3 / Sprint 1).
func (h *Handler) AiChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	wallet, token := aiAuthHeaders(r)
	if wallet == "" && token == "" {
		http.Error(w, "missing auth (Authorization or x-platarium-wallet)", http.StatusUnauthorized)
		return
	}
	if !aiRateLimitAllow(wallet, token) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	upstreamURL := strings.TrimRight(aiUpstreamURL(), "/") + "/chat/completions"
	upstreamKey := aiUpstreamKey()
	if upstreamKey == "" {
		http.Error(w, "AI upstream not configured (PLATARIUM_AI_UPSTREAM_KEY)", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	req, err := http.NewRequest(http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "build request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+upstreamKey)
	req.Header.Set("Content-Type", "application/json")
	if accept := r.Header.Get("Accept"); accept != "" {
		req.Header.Set("Accept", accept)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[AI] upstream error: %v", err)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (h *Handler) AiQuota(w http.ResponseWriter, r *http.Request) {
	wallet, _ := aiAuthHeaders(r)
	limit := aiDailyTokenLimit()
	used := aiUsageToday(wallet)
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"wallet":            wallet,
		"daily_token_limit": limit,
		"used_today":        used,
		"remaining":         remaining,
		"model_default":     aiDefaultModel(),
	})
}

func (h *Handler) AiModels(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"models": []string{aiDefaultModel()},
	})
}

func aiUpstreamURL() string {
	if v := os.Getenv("PLATARIUM_AI_UPSTREAM_URL"); v != "" {
		return v
	}
	return "https://api.openai.com/v1"
}

func aiUpstreamKey() string {
	for _, k := range []string{"PLATARIUM_AI_UPSTREAM_KEY", "OPENAI_API_KEY"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func aiDefaultModel() string {
	if v := os.Getenv("PLATARIUM_AI_DEFAULT_MODEL"); v != "" {
		return v
	}
	return "gpt-4o-mini"
}

func aiDailyTokenLimit() int64 {
	if v := os.Getenv("PLATARIUM_AI_DAILY_TOKEN_LIMIT"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 100_000
}

func aiAuthHeaders(r *http.Request) (wallet, token string) {
	wallet = strings.TrimSpace(r.Header.Get("x-platarium-wallet"))
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		token = strings.TrimSpace(auth[7:])
	}
	return wallet, token
}

var (
	aiRateMu sync.Mutex
	aiRate   = map[string][]time.Time{}
)

func aiRateLimitAllow(wallet, token string) bool {
	key := wallet
	if key == "" {
		key = token
	}
	if key == "" {
		return false
	}
	now := time.Now()
	aiRateMu.Lock()
	defer aiRateMu.Unlock()
	cutoff := now.Add(-time.Minute)
	var kept []time.Time
	for _, t := range aiRate[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= 30 {
		aiRate[key] = kept
		return false
	}
	kept = append(kept, now)
	aiRate[key] = kept
	return true
}

var (
	aiUsageMu sync.Mutex
	aiUsage   = map[string]int64{}
)

func aiUsageToday(wallet string) int64 {
	if wallet == "" {
		return 0
	}
	aiUsageMu.Lock()
	defer aiUsageMu.Unlock()
	return aiUsage[wallet]
}
