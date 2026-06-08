package nodes

import (
	"log"
	"os"
	"strconv"
)

func (nm *NodesManager) maxPeerConnections() int {
	if v := os.Getenv("PLATARIUM_MAX_PEERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 100
}

func (nm *NodesManager) recordKnownPeer(address string) {
	wsDialURL, err := normalizePeerWebSocketDialURL(address)
	if err != nil {
		return
	}
	selfWS, err := normalizePeerWebSocketDialURL(nm.nodeAddress)
	if err != nil {
		return
	}
	if wsDialURL == selfWS {
		return
	}
	nm.mu.Lock()
	if nm.knownPeerAddrs == nil {
		nm.knownPeerAddrs = make(map[string]struct{})
	}
	nm.knownPeerAddrs[wsDialURL] = struct{}{}
	nm.mu.Unlock()
}

func (nm *NodesManager) collectGossipPeers() []string {
	staticPeers := nm.loadPeers()

	nm.mu.RLock()
	defer nm.mu.RUnlock()

	seen := make(map[string]struct{})
	add := func(addr string) {
		wsDialURL, err := normalizePeerWebSocketDialURL(addr)
		if err != nil {
			return
		}
		selfWS, err := normalizePeerWebSocketDialURL(nm.nodeAddress)
		if err != nil || wsDialURL == selfWS {
			return
		}
		seen[wsDialURL] = struct{}{}
	}

	add(nm.nodeAddress)
	for _, peer := range nm.connectedNodes {
		add(peer.Address)
	}
	for addr := range nm.knownPeerAddrs {
		add(addr)
	}
	for _, addr := range staticPeers {
		add(addr)
	}

	out := make([]string, 0, len(seen))
	for addr := range seen {
		out = append(out, addr)
	}
	return out
}

func (nm *NodesManager) connectedPeerCount() int {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return len(nm.connectedNodes)
}

func (nm *NodesManager) sendPeerGossip(conn *PeerConnection) {
	if conn == nil || conn.Conn == nil {
		return
	}
	peers := nm.collectGossipPeers()
	conn.mu.Lock()
	defer conn.mu.Unlock()
	_ = conn.Conn.WriteJSON(map[string]interface{}{
		"type": "peer:gossip",
		"data": map[string]interface{}{
			"peers": peers,
		},
	})
}

func (nm *NodesManager) broadcastPeerGossip() {
	nm.mu.RLock()
	peers := make([]*PeerConnection, 0, len(nm.connectedNodes))
	for _, p := range nm.connectedNodes {
		peers = append(peers, p)
	}
	nm.mu.RUnlock()
	payload := nm.collectGossipPeers()
	msg := map[string]interface{}{
		"type": "peer:gossip",
		"data": map[string]interface{}{
			"peers": payload,
		},
	}
	for _, peer := range peers {
		peer.mu.Lock()
		_ = peer.Conn.WriteJSON(msg)
		peer.mu.Unlock()
	}
}

func (nm *NodesManager) handlePeerGossip(data map[string]interface{}) {
	raw, _ := data["peers"].([]interface{})
	if len(raw) == 0 {
		return
	}
	var newAddrs []string
	for _, item := range raw {
		addr, ok := item.(string)
		if !ok || addr == "" {
			continue
		}
		nm.recordKnownPeer(addr)
		wsDialURL, err := normalizePeerWebSocketDialURL(addr)
		if err != nil {
			continue
		}
		selfWS, err := normalizePeerWebSocketDialURL(nm.nodeAddress)
		if err != nil || wsDialURL == selfWS {
			continue
		}
		if nm.isPeerConnected(wsDialURL) {
			continue
		}
		newAddrs = append(newAddrs, wsDialURL)
	}
	if len(newAddrs) == 0 {
		return
	}
	maxPeers := nm.maxPeerConnections()
	if nm.connectedPeerCount() >= maxPeers {
		return
	}
	for _, addr := range newAddrs {
		if nm.connectedPeerCount() >= maxPeers {
			break
		}
		log.Printf("[NODE] Gossip discovery: dialing %s", addr)
		go nm.ConnectToNodeWithRetry(addr)
	}
}

func (nm *NodesManager) isPeerConnected(address string) bool {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	wsDialURL, err := normalizePeerWebSocketDialURL(address)
	if err != nil {
		return false
	}
	for _, peer := range nm.connectedNodes {
		if peerWebSocketDialURLsEqual(peer.Address, wsDialURL) {
			return true
		}
	}
	return false
}
