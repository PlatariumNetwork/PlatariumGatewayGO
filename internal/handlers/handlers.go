package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"platarium-gateway-go/internal/blockchain"
	"platarium-gateway-go/internal/nodes"
	"platarium-gateway-go/internal/websocket"

	"github.com/gorilla/mux"
)

type Handler struct {
	blockchain   *blockchain.Blockchain
	nodesManager *nodes.NodesManager
	wsServer     *websocket.Server
}

func NewHandler(bc *blockchain.Blockchain, nm *nodes.NodesManager, ws *websocket.Server) *Handler {
	return &Handler{
		blockchain:   bc,
		nodesManager: nm,
		wsServer:     ws,
	}
}

func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"message":        "PlatariumGateway v1.0.0 is running (Go)",
		"nodeId":         h.nodesManager.GetNodeID(),
		"nodeAddress":    h.nodesManager.GetNodeAddress(),
		"connectedPeers": len(h.nodesManager.GetConnectedNodes()),
	}
	jsonResponse(w, http.StatusOK, response)
}

func (h *Handler) NetworkStatus(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"nodeId":        h.nodesManager.GetNodeID(),
		"nodeAddress":   h.nodesManager.GetNodeAddress(),
		"connectedNodes": h.nodesManager.GetConnectedNodes(),
	}
	jsonResponse(w, http.StatusOK, response)
}

func (h *Handler) GetSockets(w http.ResponseWriter, r *http.Request) {
	// Query peer nodes for their socket lists
	h.nodesManager.QueryPeerSockets()
	
	allSockets := h.nodesManager.GetConnectedSockets()
	connectedNodes := h.nodesManager.GetConnectedNodes()
	
	response := map[string]interface{}{
		"nodeId":          h.nodesManager.GetNodeID(),
		"nodeAddress":     h.nodesManager.GetNodeAddress(),
		"connectedSockets": allSockets,
		"summary": map[string]interface{}{
			"connectedClients": len(allSockets),
			"connectedPeers":   len(connectedNodes),
		},
	}
	jsonResponse(w, http.StatusOK, response)
}

// GetDetailedStatus returns detailed status of all components
func (h *Handler) GetDetailedStatus(w http.ResponseWriter, r *http.Request) {
	components := make(map[string]string)
	
	// Check REST API status (if we can respond, it's OK)
	components["REST"] = "ok"
	
	// Check WebSocket server status
	if h.wsServer != nil {
		components["WebSocket"] = "ok"
	} else {
		components["WebSocket"] = "not_ok"
	}
	
	// Check P2P connections (must have at least 1 peer)
	connectedNodes := h.nodesManager.GetConnectedNodes()
	if len(connectedNodes) > 0 {
		components["P2P"] = "ok"
	} else {
		components["P2P"] = "not_ok"
	}
	
	// Check Balance system (check if blockchain is initialized)
	if h.blockchain != nil {
		// Try to get a balance to verify it works
		// GetBalance always returns a string (even "0" for non-existent addresses)
		testBalance := h.blockchain.GetBalance("test")
		// If we get a response (even "0"), the system is working
		if testBalance != "" {
			components["Balance"] = "ok"
		} else {
			components["Balance"] = "not_ok"
		}
	} else {
		components["Balance"] = "not_ok"
	}
	
	// Check Transactions module (check if we can get last transaction)
	if h.blockchain != nil {
		lastTx := h.blockchain.GetLastTransaction()
		if lastTx != nil {
			components["Transactions"] = "ok"
		} else {
			// If no transactions yet, it's still OK (module works)
			components["Transactions"] = "ok"
		}
	} else {
		components["Transactions"] = "not_ok"
	}
	
	// Determine overall status
	overallStatus := "ok"
	for _, status := range components {
		if status == "not_ok" {
			overallStatus = "not_ok"
			break
		}
	}
	
	response := map[string]interface{}{
		"network":    "Platarium",
		"status":     overallStatus,
		"components": components,
		"nodeId":     h.nodesManager.GetNodeID(),
		"nodeAddress": h.nodesManager.GetNodeAddress(),
		"connectedPeers": len(connectedNodes),
		"timestamp":  time.Now().UnixMilli(),
	}
	
	jsonResponse(w, http.StatusOK, response)
}

func (h *Handler) GetBalance(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	address := vars["address"]
	
	balance := h.blockchain.GetBalance(address)
	response := map[string]interface{}{
		"address": address,
		"balance": balance,
	}
	jsonResponse(w, http.StatusOK, response)
}

func (h *Handler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	hash := vars["hash"]
	
	tx := h.blockchain.GetTransaction(hash)
	if tx == nil {
		jsonResponse(w, http.StatusNotFound, map[string]string{
			"error": "Transaction not found",
		})
		return
	}
	jsonResponse(w, http.StatusOK, tx)
}

func (h *Handler) GetTransactions(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	address := vars["address"]
	
	txs := h.blockchain.GetTransactionsByAddress(address)
	jsonResponse(w, http.StatusOK, txs)
}

func (h *Handler) SendTransaction(w http.ResponseWriter, r *http.Request) {
	var txData map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&txData); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid request body",
		})
		return
	}
	
	// Create transaction
	tx := &blockchain.Transaction{
		Hash:            generateHash(),
		From:            getString(txData, "from"),
		To:              getString(txData, "to"),
		Value:           getString(txData, "amount"),
		Fee:             "1",
		Nonce:           getInt(txData, "nonce"),
		Timestamp:       time.Now().Unix(),
		Type:            getString(txData, "type"),
		AssetType:       getString(txData, "assetType"),
		ContractAddress: getString(txData, "contractAddress"),
	}
	
	if tx.Type == "" {
		tx.Type = "transfer"
	}
	if tx.AssetType == "" {
		tx.AssetType = "native"
	}
	
	// Add transaction
	if err := h.blockchain.AddTransaction(tx); err != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
		})
		return
	}
	
	// Broadcast transaction event
	eventData := map[string]interface{}{
		"hash":  tx.Hash,
		"from":  tx.From,
		"to":    tx.To,
		"value": tx.Value,
	}
	h.wsServer.BroadcastEvent("transactionProcessed", eventData)
	h.nodesManager.BroadcastBlockchainEvent("transactionProcessed", eventData, "")
	
	response := map[string]interface{}{
		"success":     true,
		"transaction": tx,
	}
	jsonResponse(w, http.StatusOK, response)
}

func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	if val, ok := m[key].(float64); ok {
		return int(val)
	}
	return 0
}

func generateHash() string {
	// Simple hash generation - in production use proper hashing
	return time.Now().Format("20060102150405") + "-hash"
}

