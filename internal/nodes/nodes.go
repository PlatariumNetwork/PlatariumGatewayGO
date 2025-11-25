package nodes

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
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

	connectedNodes     map[string]*PeerConnection
	peerSocketsRegistry map[string]map[string]*SocketInfo
	reconnectTimers    map[string]*time.Timer
	getLocalSockets    func() []*SocketInfo

	mu sync.RWMutex
}

// PeerConnection represents a connection to a peer node
type PeerConnection struct {
	NodeID  string
	Address string
	Host    string
	Port    int
	Conn    *websocket.Conn
	mu      sync.Mutex
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

// GetConnectedSockets returns all connected sockets (local + peer nodes)
func (nm *NodesManager) GetConnectedSockets() []*SocketInfo {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	allSockets := make([]*SocketInfo, 0)

	// Add local sockets
	if nm.getLocalSockets != nil {
		allSockets = append(allSockets, nm.getLocalSockets()...)
	}

	// Add peer node sockets
	for _, socketsMap := range nm.peerSocketsRegistry {
		for _, socket := range socketsMap {
			allSockets = append(allSockets, socket)
		}
	}

	return allSockets
}

// ConnectToPeers connects to all peer nodes from configuration
func (nm *NodesManager) ConnectToPeers() {
	peers := nm.loadPeers()
	for _, peerAddr := range peers {
		if peerAddr != nm.nodeAddress {
			go nm.ConnectToNode(peerAddr)
		}
	}
}

// ConnectToNode connects to a specific peer node
func (nm *NodesManager) ConnectToNode(address string) {
	if address == nm.nodeAddress {
		return
	}

	nm.mu.Lock()
	// Check if already connected
	for _, peer := range nm.connectedNodes {
		if peer.Address == address {
			nm.mu.Unlock()
			return
		}
	}
	nm.mu.Unlock()

	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}

	conn, _, err := dialer.Dial(address, nil)
	if err != nil {
		log.Printf("[NODE] Connection error to %s: %v", address, err)
		nm.scheduleReconnect(address)
		return
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
}

func (nm *NodesManager) handlePeerConnection(conn *websocket.Conn, address string) {
	defer conn.Close()

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
		}
	}

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

	nm.mu.Lock()
	nm.connectedNodes[nodeID] = &PeerConnection{
		NodeID:  nodeID,
		Address: peerAddr,
		Host:    host,
		Port:    int(port),
		Conn:    conn,
	}
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
	if originNodeID == nm.nodeID {
		return // Prevent loops
	}

	// Re-broadcast to other nodes
	eventType, _ := data["type"].(string)
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

	event := map[string]interface{}{
		"type":         "blockchain:event",
		"eventType":    eventType,
		"data":         data,
		"timestamp":    time.Now().Unix(),
		"originNodeId": originNodeID,
	}

	count := 0
	for nodeID, peer := range nm.connectedNodes {
		if originNodeID != "" && nodeID == originNodeID {
			continue
		}

		peer.mu.Lock()
		if peer.Conn != nil {
			if err := peer.Conn.WriteJSON(event); err == nil {
				count++
			}
		}
		peer.mu.Unlock()
	}

	if count > 0 {
		log.Printf("[NODE] Broadcasted %s to %d node(s)", eventType, count)
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

