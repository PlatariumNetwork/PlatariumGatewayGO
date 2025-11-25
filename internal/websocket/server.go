package websocket

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"platarium-gateway-go/internal/blockchain"
	"platarium-gateway-go/internal/nodes"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins
	},
}

// Server represents the WebSocket server
type Server struct {
	port         int
	blockchain   *blockchain.Blockchain
	nodesManager *nodes.NodesManager
	clients      map[string]*Client
	mu           sync.RWMutex
	server       *http.Server
}

// Client represents a WebSocket client connection
type Client struct {
	ID          string
	Conn        *websocket.Conn
	IPAddress   string
	ConnectedAt time.Time
	mu          sync.Mutex
}

// NewServer creates a new WebSocket server
func NewServer(port int, bc *blockchain.Blockchain, nm *nodes.NodesManager) *Server {
	s := &Server{
		port:         port,
		blockchain:   bc,
		nodesManager: nm,
		clients:      make(map[string]*Client),
	}

	// Set local sockets getter
	nm.SetLocalSocketsGetter(s.GetConnectedSockets)

	return s
}

// Start starts the WebSocket server
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleWebSocket)

	s.server = &http.Server{
		Addr:    fmt.Sprintf("0.0.0.0:%d", s.port), // Listen on all interfaces
		Handler: mux,
	}

	log.Printf("[WS] WebSocket server listening on 0.0.0.0:%d (all interfaces)", s.port)
	return s.server.ListenAndServe()
}

// Stop stops the WebSocket server
func (s *Server) Stop() {
	if s.server != nil {
		s.server.Close()
	}
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	clientID := uuid.New().String()
	clientIP := r.RemoteAddr
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		clientIP = forwarded
	}

	client := &Client{
		ID:          clientID,
		Conn:        conn,
		IPAddress:   clientIP,
		ConnectedAt: time.Now(),
	}

	s.mu.Lock()
	s.clients[clientID] = client
	s.mu.Unlock()

	log.Printf("[SOCKET] Client connected: %s (IP: %s)", clientID, clientIP)

	// Announce client connection to peer nodes
	if s.nodesManager != nil {
		connectedAt := client.ConnectedAt.Format(time.RFC3339)
		s.nodesManager.AnnounceClientConnected(clientID, clientIP, connectedAt)
	}

	// Handle peer node announcements
	if s.nodesManager != nil {
		go s.handlePeerMessages(conn, clientID)
	}

	// Handle client messages
	go s.handleClientMessages(client)

	// Cleanup on disconnect
	defer func() {
		s.mu.Lock()
		delete(s.clients, clientID)
		s.mu.Unlock()

		log.Printf("[SOCKET] Client disconnected: %s", clientID)

		if s.nodesManager != nil {
			s.nodesManager.AnnounceClientDisconnected(clientID)
		}
	}()
}

func (s *Server) handlePeerMessages(conn *websocket.Conn, clientID string) {
	for {
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			break
		}

		msgType, _ := msg["type"].(string)
		data, _ := msg["data"].(map[string]interface{})

		switch msgType {
		case "node:announce":
			// This is handled by nodes manager
		case "sockets:request":
			s.handleSocketsRequest(conn)
		case "sockets:response":
			if s.nodesManager != nil {
				s.nodesManager.HandleSocketsResponse(data)
			}
		case "blockchain:event":
			// Handle blockchain events from peers
			eventType, _ := data["eventType"].(string)
			eventData, _ := data["data"].(map[string]interface{})
			s.BroadcastToClients(eventType, eventData)
		}
	}
}

func (s *Server) handleClientMessages(client *Client) {
	for {
		var msg map[string]interface{}
		if err := client.Conn.ReadJSON(&msg); err != nil {
			break
		}

		msgType, _ := msg["type"].(string)
		data, _ := msg["data"].(map[string]interface{})

		switch msgType {
		case "newTransaction":
			s.handleNewTransaction(data)
		}
	}
}

func (s *Server) handleNewTransaction(data map[string]interface{}) {
	from, _ := data["from"].(string)
	to, _ := data["to"].(string)
	amount, _ := data["amount"].(string)

	log.Printf("Processing transaction: from %s to %s amount %s", from, to, amount)

	// Create transaction
	tx := &blockchain.Transaction{
		Hash:      uuid.New().String(),
		From:      from,
		To:        to,
		Value:     amount,
		Fee:       "1",
		Timestamp: time.Now().Unix(),
		Type:      "transfer",
		AssetType: "native",
	}

	// Add to blockchain
	if err := s.blockchain.AddTransaction(tx); err != nil {
		s.BroadcastToClients("transactionError", map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	log.Printf("Transaction added with hash: %s, Fee: %s coreon", tx.Hash, tx.Fee)

	eventData := map[string]interface{}{
		"hash":  tx.Hash,
		"from":  tx.From,
		"to":    tx.To,
		"value": tx.Value,
	}

	// Broadcast to local clients
	s.BroadcastToClients("transactionProcessed", eventData)

	// Broadcast to peer nodes
	if s.nodesManager != nil {
		s.nodesManager.BroadcastBlockchainEvent("transactionProcessed", eventData, "")
	}
}

func (s *Server) handleSocketsRequest(conn *websocket.Conn) {
	sockets := s.GetConnectedSockets()
	conn.WriteJSON(map[string]interface{}{
		"type": "sockets:response",
		"data": map[string]interface{}{
			"nodeId":  s.nodesManager.GetNodeID(),
			"sockets": sockets,
		},
	})
}

// BroadcastToClients broadcasts an event to all connected clients
func (s *Server) BroadcastToClients(eventType string, data map[string]interface{}) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	message := map[string]interface{}{
		"type": eventType,
		"data": data,
	}

	for _, client := range s.clients {
		client.mu.Lock()
		if err := client.Conn.WriteJSON(message); err != nil {
			log.Printf("Error broadcasting to client %s: %v", client.ID, err)
		}
		client.mu.Unlock()
	}
}

// BroadcastEvent broadcasts an event (alias for BroadcastToClients)
func (s *Server) BroadcastEvent(eventType string, data map[string]interface{}) {
	s.BroadcastToClients(eventType, data)
}

// GetConnectedSockets returns all connected client sockets
func (s *Server) GetConnectedSockets() []*nodes.SocketInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sockets := make([]*nodes.SocketInfo, 0, len(s.clients))
	for _, client := range s.clients {
		sockets = append(sockets, &nodes.SocketInfo{
			SocketID:    client.ID,
			IPAddress:   client.IPAddress,
			ConnectedAt: client.ConnectedAt.Format(time.RFC3339),
			NodeID:      s.nodesManager.GetNodeID(),
		})
	}
	return sockets
}

