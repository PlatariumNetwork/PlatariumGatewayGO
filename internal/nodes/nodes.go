package nodes

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// NodeInfo represents information about a connected peer node
type NodeInfo struct {
	NodeID  string `json:"nodeId"`
	Address string `json:"address"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
}

// SocketInfo represents information about a connected client socket
type SocketInfo struct {
	SocketID    string `json:"socketId"`
	IPAddress   string `json:"ipAddress"`
	ConnectedAt string `json:"connectedAt"`
	NodeID      string `json:"nodeId"`
}

// NodesManager manages peer node connections
type NodesManager struct {
	nodeID      string
	nodeHost    string
	nodePort    int
	nodeAddress string

	connectedNodes      map[string]*PeerConnection
	peerSocketsRegistry map[string]map[string]*SocketInfo
	reconnectTimers     map[string]*time.Timer
	getLocalSockets     func() []*SocketInfo
	seenEvents          map[string]time.Time // For duplicate detection
	eventMutex          sync.RWMutex

	mu sync.RWMutex
}

// Event represents a blockchain event
type Event struct {
	Type      string      `json:"type"`
	Data      interface{} `json:"data"`
	Timestamp int64      `json:"timestamp"`
	OriginID  string      `json:"originId"`
	EventID   string      `json:"eventId"` // For duplicate detection
}

// PeerConnection represents a connection to a peer node
type PeerConnection struct {
	NodeID        string
	Address       string
	Host          string
	Port          int
	Conn          *websocket.Conn
	LastPong      time.Time
	PingFailures  int
	HeartbeatStop chan bool
	mu            sync.Mutex
}

// NewNodesManager creates a new nodes manager
func NewNodesManager(port int, host string) *NodesManager {
	nodeID := uuid.New().String()
	return &NodesManager{
		nodeID:              nodeID,
		nodeHost:            host,
		nodePort:            port,
		nodeAddress:         fmt.Sprintf("ws://%s:%d", host, port),
		connectedNodes:      make(map[string]*PeerConnection),
		peerSocketsRegistry: make(map[string]map[string]*SocketInfo),
		reconnectTimers:     make(map[string]*time.Timer),
		seenEvents:          make(map[string]time.Time),
	}
}

// GetNodeID returns the node's unique ID
func (nm *NodesManager) GetNodeID() string {
	return nm.nodeID
}

// GetNodeAddress returns the node's WebSocket address
func (nm *NodesManager) GetNodeAddress() string {
	return nm.nodeAddress
}

// SetLocalSocketsGetter sets the function to get local sockets
func (nm *NodesManager) SetLocalSocketsGetter(getter func() []*SocketInfo) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.getLocalSockets = getter
}

// GetConnectedNodes returns all connected peer nodes
func (nm *NodesManager) GetConnectedNodes() []NodeInfo {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	nodes := make([]NodeInfo, 0, len(nm.connectedNodes))
	for _, peer := range nm.connectedNodes {
		nodes = append(nodes, NodeInfo{
			NodeID:  peer.NodeID,
			Address: peer.Address,
			Host:    peer.Host,
			Port:    peer.Port,
		})
	}
	return nodes
}

// PeerWithPing represents a peer node with ping information
type PeerWithPing struct {
	NodeID  string `json:"nodeId"`
	Address string `json:"address"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Ping    *int64 `json:"ping,omitempty"` // Ping in milliseconds, nil if unknown
}

// GetPeersWithPing returns all connected peer nodes with ping information
func (nm *NodesManager) GetPeersWithPing() []PeerWithPing {
	nm.mu.RLock()
	peersList := make([]*PeerConnection, 0, len(nm.connectedNodes))
	for _, peer := range nm.connectedNodes {
		peersList = append(peersList, peer)
	}
	nm.mu.RUnlock()

	peers := make([]PeerWithPing, 0, len(peersList))
	
	// Use a channel to collect ping results concurrently
	type pingResult struct {
		index int
		ping  *int64
	}
	pingChan := make(chan pingResult, len(peersList))
	
	// Measure ping for each peer concurrently
	for i, peer := range peersList {
		go func(idx int, p *PeerConnection) {
			var ping *int64
			
			if p.Conn != nil {
				// Try to measure actual ping by making HTTP request
				httpAddr := p.Address
				if strings.HasPrefix(httpAddr, "ws://") {
					httpAddr = "http://" + httpAddr[5:]
				} else if strings.HasPrefix(httpAddr, "wss://") {
					httpAddr = "https://" + httpAddr[6:]
				}
				
				client := &http.Client{
					Timeout: 3 * time.Second,
				}
				
				start := time.Now()
				resp, err := client.Get(httpAddr + "/api")
				if err == nil && resp != nil {
					resp.Body.Close()
					pingMs := time.Since(start).Milliseconds()
					ping = &pingMs
				}
			}
			
			pingChan <- pingResult{index: idx, ping: ping}
		}(i, peer)
	}
	
	// Collect results with timeout
	results := make(map[int]*int64)
	timeout := time.After(2 * time.Second)
	
	for i := 0; i < len(peersList); i++ {
		select {
		case result := <-pingChan:
			results[result.index] = result.ping
		case <-timeout:
			break
		}
	}
	
	// Build final list with ping results
	for i, peer := range peersList {
		ping := results[i]
		peers = append(peers, PeerWithPing{
			NodeID:  peer.NodeID,
			Address: peer.Address,
			Host:    peer.Host,
			Port:    peer.Port,
			Ping:    ping,
		})
	}
	
	return peers
}

// GetConnectedSockets returns all connected sockets (local + peer nodes)
func (nm *NodesManager) GetConnectedSockets() []*SocketInfo {
	allSockets := make([]*SocketInfo, 0)

	// Add local sockets (no lock needed for getter function)
	if nm.getLocalSockets != nil {
		allSockets = append(allSockets, nm.getLocalSockets()...)
	}

	// Add peer node sockets
	nm.mu.RLock()
	peerSocketsCopy := make(map[string]map[string]*SocketInfo)
	for nodeID, socketsMap := range nm.peerSocketsRegistry {
		peerSocketsCopy[nodeID] = make(map[string]*SocketInfo)
		for socketID, socket := range socketsMap {
			peerSocketsCopy[nodeID][socketID] = socket
		}
	}
	nm.mu.RUnlock()

	for _, socketsMap := range peerSocketsCopy {
		for _, socket := range socketsMap {
			allSockets = append(allSockets, socket)
		}
	}

	return allSockets
}

// ConnectToPeers connects to all peer nodes from configuration
func (nm *NodesManager) ConnectToPeers() {
	peers := nm.loadPeers()
	log.Printf("[NODE] Connecting to %d peer(s)...", len(peers))
	for _, peerAddr := range peers {
		if peerAddr != nm.nodeAddress {
			go nm.ConnectToNodeWithRetry(peerAddr)
		}
	}
}

// ConnectToNodeWithRetry connects to a peer node with automatic retry on failure
func (nm *NodesManager) ConnectToNodeWithRetry(address string) {
	for {
		err := nm.tryConnect(address)
		if err == nil {
			break // Successfully connected
		}
		log.Printf("[NODE] Failed to connect to %s: %v. Retrying in 5s...", address, err)
		time.Sleep(5 * time.Second)
	}
}

// tryConnect attempts to connect to a peer node
func (nm *NodesManager) tryConnect(address string) error {
	if address == nm.nodeAddress {
		return fmt.Errorf("cannot connect to self")
	}

	parsedURL, err := url.Parse(address)
	if err != nil {
		return err
	}
	query := parsedURL.Query()
	query.Set("peer", "1")
	parsedURL.RawQuery = query.Encode()
	addressWithPeer := parsedURL.String()

	nm.mu.Lock()
	// Check if already connected
	for _, peer := range nm.connectedNodes {
		if peer.Address == address {
			nm.mu.Unlock()
			return nil // Already connected
		}
	}
	nm.mu.Unlock()

	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}

	conn, _, err := dialer.Dial(addressWithPeer, nil)
	if err != nil {
		return err
	}

	log.Printf("[NODE] Connected to %s", address)

	// Send node announcement
	announcement := map[string]interface{}{
		"nodeId":  nm.nodeID,
		"address": nm.nodeAddress,
		"host":    nm.nodeHost,
		"port":    nm.nodePort,
	}
	conn.WriteJSON(map[string]interface{}{
		"type": "node:announce",
		"data": announcement,
	})

	// Handle the connection
	go nm.handlePeerConnection(conn, address)
	return nil
}

// ConnectToNode connects to a specific peer node (legacy method, uses tryConnect)
func (nm *NodesManager) ConnectToNode(address string) {
	if err := nm.tryConnect(address); err != nil {
		log.Printf("[NODE] Connection error to %s: %v", address, err)
		nm.scheduleReconnect(address)
	}
}

// HandleIncomingPeer handles inbound peer WebSocket connections
func (nm *NodesManager) HandleIncomingPeer(conn *websocket.Conn, address string) {
	go nm.handlePeerConnection(conn, address)
}

func (nm *NodesManager) handlePeerConnection(conn *websocket.Conn, address string) {
	defer conn.Close()

	// Set up heartbeat/ping-pong
	conn.SetPongHandler(func(string) error {
		nm.mu.Lock()
		for _, peer := range nm.connectedNodes {
			if peer.Address == address {
				peer.LastPong = time.Now()
				peer.PingFailures = 0
				break
			}
		}
		nm.mu.Unlock()
		return nil
	})

	// Start heartbeat ticker
	heartbeatTicker := time.NewTicker(10 * time.Second)
	defer heartbeatTicker.Stop()

	// Start heartbeat goroutine
	heartbeatStop := make(chan bool)
	go func() {
		for {
			select {
			case <-heartbeatTicker.C:
				nm.mu.RLock()
				var peer *PeerConnection
				for _, p := range nm.connectedNodes {
					if p.Address == address {
						peer = p
						break
					}
				}
				nm.mu.RUnlock()

				if peer != nil {
					peer.mu.Lock()
					if peer.Conn != nil {
						// Send ping
						if err := peer.Conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
							peer.mu.Unlock()
							return
						}
						peer.mu.Unlock()

						// Check for pong response
						nm.mu.Lock()
						for _, p := range nm.connectedNodes {
							if p.Address == address {
								if time.Since(p.LastPong) > 30*time.Second {
									p.PingFailures++
									if p.PingFailures > 2 {
										log.Printf("[NODE] Too many ping failures for %s, reconnecting...", address)
										nm.mu.Unlock()
										return
									}
								}
								break
							}
						}
						nm.mu.Unlock()
					} else {
						peer.mu.Unlock()
					}
				}
			case <-heartbeatStop:
				return
			}
		}
	}()

	// Message reading loop
	for {
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			log.Printf("[NODE] Error reading from peer %s: %v", address, err)
			break
		}

		msgType, _ := msg["type"].(string)
		data, _ := msg["data"].(map[string]interface{})

		switch msgType {
		case "node:announce":
			nm.handleNodeAnnounce(data, conn)
		case "client:connected":
			nm.handleClientConnected(data)
		case "client:disconnected":
			nm.handleClientDisconnected(data)
		case "blockchain:event":
			nm.handleBlockchainEvent(data)
		case "sockets:request":
			nm.handleSocketsRequest(conn)
		case "sockets:response":
			nm.HandleSocketsResponse(data)
		case "sync:request":
			nm.handleSyncRequest(conn)
		case "sync:response":
			nm.handleSyncResponse(data)
		case "ping":
			// Respond to ping
			conn.WriteJSON(map[string]interface{}{
				"type": "pong",
			})
		case "pong":
			// Pong received, already handled by SetPongHandler
		}
	}

	// Stop heartbeat
	close(heartbeatStop)

	// Clean up on disconnect
	nm.mu.Lock()
	for nodeID, peer := range nm.connectedNodes {
		if peer.Address == address {
			delete(nm.connectedNodes, nodeID)
			delete(nm.peerSocketsRegistry, nodeID)
			log.Printf("[NODE] Disconnected: %s", nodeID[:8])
			break
		}
	}
	nm.mu.Unlock()

	nm.scheduleReconnect(address)
}

func (nm *NodesManager) handleNodeAnnounce(data map[string]interface{}, conn *websocket.Conn) {
	nodeID, _ := data["nodeId"].(string)
	peerAddr, _ := data["address"].(string)
	host, _ := data["host"].(string)
	port, _ := data["port"].(float64)

	if nodeID == nm.nodeID {
		return
	}

	// Verify nodeID before adding
	if nodeID == "" {
		log.Printf("[NODE] Rejected peer connection: empty nodeID")
		return
	}

	nm.mu.Lock()
	// Check if already exists
	if existing, exists := nm.connectedNodes[nodeID]; exists {
		// Update connection if different
		if existing.Address != peerAddr {
			existing.Conn = conn
			existing.Address = peerAddr
			existing.Host = host
			existing.Port = int(port)
			existing.LastPong = time.Now()
			existing.PingFailures = 0
		}
		nm.mu.Unlock()
		return
	}

	// Create new peer connection
	peer := &PeerConnection{
		NodeID:        nodeID,
		Address:       peerAddr,
		Host:          host,
		Port:          int(port),
		Conn:          conn,
		LastPong:      time.Now(),
		PingFailures:  0,
		HeartbeatStop: make(chan bool),
	}
	nm.connectedNodes[nodeID] = peer
	nm.mu.Unlock()

	log.Printf("[NODE] Peer node registered: %s... at %s", nodeID[:8], peerAddr)

	// Send our own announcement back
	conn.WriteJSON(map[string]interface{}{
		"type": "node:announce",
		"data": map[string]interface{}{
			"nodeId":  nm.nodeID,
			"address": nm.nodeAddress,
			"host":    nm.nodeHost,
			"port":    nm.nodePort,
		},
	})

	// Request socket list
	conn.WriteJSON(map[string]interface{}{
		"type": "sockets:request",
	})

	// Send blockchain state sync request
	conn.WriteJSON(map[string]interface{}{
		"type": "sync:request",
	})
}

func (nm *NodesManager) handleClientConnected(data map[string]interface{}) {
	nodeID, _ := data["nodeId"].(string)
	clientID, _ := data["clientId"].(string)
	ip, _ := data["ip"].(string)
	connectedAt, _ := data["connectedAt"].(string)

	if nodeID == nm.nodeID {
		return
	}

	if connectedAt == "" {
		connectedAt = time.Now().Format(time.RFC3339)
	}

	nm.mu.Lock()
	if nm.peerSocketsRegistry[nodeID] == nil {
		nm.peerSocketsRegistry[nodeID] = make(map[string]*SocketInfo)
	}
	nm.peerSocketsRegistry[nodeID][clientID] = &SocketInfo{
		SocketID:    clientID,
		IPAddress:   ip,
		ConnectedAt: connectedAt,
		NodeID:      nodeID,
	}
	nm.mu.Unlock()

	log.Printf("[WS] New client connected on %s... (IP: %s)", nodeID[:8], ip)
}

func (nm *NodesManager) handleClientDisconnected(data map[string]interface{}) {
	nodeID, _ := data["nodeId"].(string)
	clientID, _ := data["clientId"].(string)

	if nodeID == nm.nodeID {
		return
	}

	nm.mu.Lock()
	if socketsMap, exists := nm.peerSocketsRegistry[nodeID]; exists {
		delete(socketsMap, clientID)
		if len(socketsMap) == 0 {
			delete(nm.peerSocketsRegistry, nodeID)
		}
	}
	nm.mu.Unlock()

	log.Printf("[WS] Client disconnected on %s...", nodeID[:8])
}

func (nm *NodesManager) handleBlockchainEvent(data map[string]interface{}) {
	originNodeID, _ := data["originNodeId"].(string)
	eventID, _ := data["eventId"].(string)
	
	if originNodeID == nm.nodeID {
		return // Prevent loops
	}

	// Check for duplicate events
	if eventID != "" {
		nm.eventMutex.Lock()
		if seen, exists := nm.seenEvents[eventID]; exists {
			// Event seen within last 5 minutes, ignore
			if time.Since(seen) < 5*time.Minute {
				nm.eventMutex.Unlock()
				log.Printf("[NODE] Duplicate event ignored: %s", eventID[:8])
				return
			}
		}
		nm.seenEvents[eventID] = time.Now()
		
		// Clean old events (older than 10 minutes)
		for id, t := range nm.seenEvents {
			if time.Since(t) > 10*time.Minute {
				delete(nm.seenEvents, id)
			}
		}
		nm.eventMutex.Unlock()
	}

	// Re-broadcast to other nodes
	eventType, _ := data["eventType"].(string)
	if eventType == "" {
		eventType, _ = data["type"].(string) // Fallback
	}
	nm.BroadcastBlockchainEvent(eventType, data, originNodeID)
}

func (nm *NodesManager) handleSocketsRequest(conn *websocket.Conn) {
	if nm.getLocalSockets == nil {
		return
	}

	localSockets := nm.getLocalSockets()
	conn.WriteJSON(map[string]interface{}{
		"type": "sockets:response",
		"data": map[string]interface{}{
			"nodeId":  nm.nodeID,
			"sockets": localSockets,
		},
	})
}

// HandleSocketsResponse handles socket list responses from peer nodes
func (nm *NodesManager) HandleSocketsResponse(data map[string]interface{}) {
	nodeID, _ := data["nodeId"].(string)
	sockets, _ := data["sockets"].([]interface{})

	if nodeID == nm.nodeID {
		return
	}

	nm.mu.Lock()
	socketsMap := make(map[string]*SocketInfo)
	for _, s := range sockets {
		if socketData, ok := s.(map[string]interface{}); ok {
			socketID, _ := socketData["socketId"].(string)
			ip, _ := socketData["ipAddress"].(string)
			connectedAt, _ := socketData["connectedAt"].(string)
			socketNodeID, _ := socketData["nodeId"].(string)

			socketsMap[socketID] = &SocketInfo{
				SocketID:    socketID,
				IPAddress:   ip,
				ConnectedAt: connectedAt,
				NodeID:      socketNodeID,
			}
		}
	}
	nm.peerSocketsRegistry[nodeID] = socketsMap
	nm.mu.Unlock()
}

// BroadcastBlockchainEvent broadcasts a blockchain event to all peer nodes
func (nm *NodesManager) BroadcastBlockchainEvent(eventType string, data map[string]interface{}, originNodeID string) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	// Generate unique event ID for duplicate detection
	eventID := fmt.Sprintf("%s-%d-%s", eventType, time.Now().UnixNano(), nm.nodeID)

	event := map[string]interface{}{
		"type":         "blockchain:event",
		"eventType":    eventType,
		"data":         data,
		"timestamp":    time.Now().Unix(),
		"originNodeId": originNodeID,
		"eventId":      eventID,
	}

	// Mark as seen by us
	nm.eventMutex.Lock()
	nm.seenEvents[eventID] = time.Now()
	nm.eventMutex.Unlock()

	count := 0
	for nodeID, peer := range nm.connectedNodes {
		if originNodeID != "" && nodeID == originNodeID {
			continue
		}

		peer.mu.Lock()
		if peer.Conn != nil {
			if err := peer.Conn.WriteJSON(event); err == nil {
				count++
			} else {
				log.Printf("[NODE] Error broadcasting to %s: %v", nodeID[:8], err)
			}
		}
		peer.mu.Unlock()
	}

	if count > 0 {
		log.Printf("[NODE] Broadcasted %s (ID: %s) to %d node(s)", eventType, eventID[:8], count)
	}
}

// AnnounceClientConnected announces a client connection to all peer nodes
func (nm *NodesManager) AnnounceClientConnected(clientID, ip, connectedAt string) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	announcement := map[string]interface{}{
		"type": "client:connected",
		"data": map[string]interface{}{
			"nodeId":     nm.nodeID,
			"clientId":   clientID,
			"ip":         ip,
			"connectedAt": connectedAt,
		},
	}

	for _, peer := range nm.connectedNodes {
		peer.mu.Lock()
		if peer.Conn != nil {
			peer.Conn.WriteJSON(announcement)
		}
		peer.mu.Unlock()
	}
}

// AnnounceClientDisconnected announces a client disconnection to all peer nodes
func (nm *NodesManager) AnnounceClientDisconnected(clientID string) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	announcement := map[string]interface{}{
		"type": "client:disconnected",
		"data": map[string]interface{}{
			"nodeId":   nm.nodeID,
			"clientId": clientID,
		},
	}

	for _, peer := range nm.connectedNodes {
		peer.mu.Lock()
		if peer.Conn != nil {
			peer.Conn.WriteJSON(announcement)
		}
		peer.mu.Unlock()
	}
}

// QueryPeerSockets queries all peer nodes for their socket lists
func (nm *NodesManager) QueryPeerSockets() {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	request := map[string]interface{}{
		"type": "sockets:request",
	}

	for _, peer := range nm.connectedNodes {
		peer.mu.Lock()
		if peer.Conn != nil {
			peer.Conn.WriteJSON(request)
		}
		peer.mu.Unlock()
	}

	time.Sleep(500 * time.Millisecond) // Wait for responses
}

func (nm *NodesManager) scheduleReconnect(address string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if _, exists := nm.reconnectTimers[address]; exists {
		return
	}

	timer := time.AfterFunc(5*time.Second, func() {
		nm.mu.Lock()
		delete(nm.reconnectTimers, address)
		nm.mu.Unlock()
		nm.ConnectToNode(address)
	})

	nm.reconnectTimers[address] = timer
}

func (nm *NodesManager) loadPeers() []string {
	peers := []string{}

	// Try to load from peers.json
	peersPath := filepath.Join(".", "peers.json")
	if data, err := ioutil.ReadFile(peersPath); err == nil {
		var config struct {
			Peers []string `json:"peers"`
		}
		if json.Unmarshal(data, &config) == nil {
			peers = append(peers, config.Peers...)
		}
	}

	// Try to load from environment variable
	if envPeers := os.Getenv("PEERS"); envPeers != "" {
		var envPeerList []string
		if json.Unmarshal([]byte(envPeers), &envPeerList) == nil {
			peers = append(peers, envPeerList...)
		}
	}

	// Filter out self
	filtered := []string{}
	for _, peer := range peers {
		if peer != nm.nodeAddress {
			filtered = append(filtered, peer)
		}
	}

	return filtered
}

// handleSyncRequest handles blockchain state sync requests
func (nm *NodesManager) handleSyncRequest(conn *websocket.Conn) {
	// Send blockchain state (transactions, balances, etc.)
	// This is a placeholder - in production, send actual blockchain state
	conn.WriteJSON(map[string]interface{}{
		"type": "sync:response",
		"data": map[string]interface{}{
			"nodeId":     nm.nodeID,
			"timestamp":  time.Now().Unix(),
			"blockchain": "synced", // Placeholder
		},
	})
	log.Printf("[NODE] Sent sync response to peer")
}

// handleSyncResponse handles blockchain state sync responses
func (nm *NodesManager) handleSyncResponse(data map[string]interface{}) {
	nodeID, _ := data["nodeId"].(string)
	log.Printf("[NODE] Received sync response from %s...", nodeID[:8])
	// In production, merge blockchain state here
}

// GetMetrics returns monitoring metrics
func (nm *NodesManager) GetMetrics() map[string]interface{} {
	nm.mu.RLock()
	peersList := make([]*PeerConnection, 0, len(nm.connectedNodes))
	for _, peer := range nm.connectedNodes {
		peersList = append(peersList, peer)
	}
	connectedPeersCount := len(nm.connectedNodes)
	nm.mu.RUnlock()

	peers := make([]map[string]interface{}, 0)
	for _, peer := range peersList {
		peer.mu.Lock()
		lastPing := int64(0)
		if !peer.LastPong.IsZero() {
			lastPing = time.Since(peer.LastPong).Milliseconds()
		}
		pingFailures := peer.PingFailures
		peerAddress := peer.Address
		peerNodeID := peer.NodeID
		peer.mu.Unlock()
		
		peers = append(peers, map[string]interface{}{
			"nodeId":       peerNodeID,
			"address":     peerAddress,
			"pingFailures": pingFailures,
			"lastPong":    lastPing,
		})
	}

	nm.eventMutex.RLock()
	seenEventsCount := len(nm.seenEvents)
	nm.eventMutex.RUnlock()

	// Get connected clients count (without holding main lock)
	connectedClientsCount := len(nm.GetConnectedSockets())

	return map[string]interface{}{
		"connectedPeers":   connectedPeersCount,
		"connectedClients": connectedClientsCount,
		"peers":            peers,
		"seenEvents":       seenEventsCount,
	}
}

