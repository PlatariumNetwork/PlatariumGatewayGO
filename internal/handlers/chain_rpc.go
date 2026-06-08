package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"platarium-gateway-go/internal/blockchain"
)

const platariumChainID = "0x7070" // "pp" testnet placeholder

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type jsonRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *jsonRPCError `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ChainRPC handles POST /rpc/v1 JSON-RPC 2.0 chain queries for wallets/explorers.
func (h *Handler) ChainRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONRPCError(w, nil, -32700, "failed to read body")
		return
	}
	var req jsonRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONRPCError(w, nil, -32700, "parse error")
		return
	}
	if req.JSONRPC != "" && req.JSONRPC != "2.0" {
		writeJSONRPCError(w, req.ID, -32600, "invalid jsonrpc version")
		return
	}
	result, rpcErr := h.dispatchChainRPC(req.Method, req.Params)
	if rpcErr != nil {
		writeJSONRPC(w, req.ID, nil, rpcErr)
		return
	}
	writeJSONRPC(w, req.ID, result, nil)
}

func writeJSONRPC(w http.ResponseWriter, id interface{}, result interface{}, rpcErr *jsonRPCError) {
	w.Header().Set("Content-Type", "application/json")
	resp := jsonRPCResponse{JSONRPC: "2.0", ID: id, Result: result, Error: rpcErr}
	_ = json.NewEncoder(w).Encode(resp)
}

func writeJSONRPCError(w http.ResponseWriter, id interface{}, code int, msg string) {
	writeJSONRPC(w, id, nil, &jsonRPCError{Code: code, Message: msg})
}

func (h *Handler) dispatchChainRPC(method string, params json.RawMessage) (interface{}, *jsonRPCError) {
	switch method {
	case "platarium_chainId", "eth_chainId":
		return platariumChainID, nil
	case "platarium_net_version", "net_version":
		return "7070", nil
	case "platarium_blockNumber", "eth_blockNumber":
		head := h.blockchain.HeadBlockNumber()
		if head < 0 {
			return "0x0", nil
		}
		return fmt.Sprintf("0x%x", head), nil
	case "platarium_getBalance", "eth_getBalance":
		var args []interface{}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &args)
		}
		if len(args) < 1 {
			return nil, &jsonRPCError{Code: -32602, Message: "address required"}
		}
		address, _ := args[0].(string)
		account, err := h.blockchain.GetAccountQuery(address)
		if err != nil {
			return nil, &jsonRPCError{Code: -32000, Message: err.Error()}
		}
		bal, err := strconv.ParseUint(account.Balance, 10, 64)
		if err != nil {
			return nil, &jsonRPCError{Code: -32000, Message: "invalid balance"}
		}
		return fmt.Sprintf("0x%x", bal), nil
	case "platarium_getBlockByNumber", "eth_getBlockByNumber":
		return h.rpcGetBlockByNumber(params)
	case "platarium_getBlockByHash", "eth_getBlockByHash":
		return h.rpcGetBlockByHash(params)
	case "platarium_getTransactionByHash", "eth_getTransactionByHash":
		return h.rpcGetTransactionByHash(params)
	case "platarium_getBlockTransactionCountByNumber":
		return h.rpcGetBlockTxCount(params)
	default:
		return nil, &jsonRPCError{Code: -32601, Message: "method not found: " + method}
	}
}

func (h *Handler) rpcGetBlockByNumber(params json.RawMessage) (interface{}, *jsonRPCError) {
	var args []interface{}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &args)
	}
	if len(args) < 1 {
		return nil, &jsonRPCError{Code: -32602, Message: "block number required"}
	}
	num, err := h.parseBlockNumberArg(args[0])
	if err != nil {
		return nil, &jsonRPCError{Code: -32602, Message: err.Error()}
	}
	block := h.blockchain.GetBlockByNumber(num)
	if block == nil {
		return nil, nil
	}
	return blockToRPC(block), nil
}

func (h *Handler) rpcGetBlockByHash(params json.RawMessage) (interface{}, *jsonRPCError) {
	var args []interface{}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &args)
	}
	if len(args) < 1 {
		return nil, &jsonRPCError{Code: -32602, Message: "block hash required"}
	}
	hash, _ := args[0].(string)
	hash = strings.TrimPrefix(strings.ToLower(hash), "0x")
	for _, b := range h.blockchain.GetBlockHistory() {
		bh := strings.TrimPrefix(strings.ToLower(b.BlockHash), "0x")
		if bh == hash {
			copy := b
			return blockToRPC(&copy), nil
		}
	}
	return nil, nil
}

func (h *Handler) rpcGetTransactionByHash(params json.RawMessage) (interface{}, *jsonRPCError) {
	var args []interface{}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &args)
	}
	if len(args) < 1 {
		return nil, &jsonRPCError{Code: -32602, Message: "tx hash required"}
	}
	hash, _ := args[0].(string)
	tx := h.blockchain.GetTransaction(hash)
	if tx == nil {
		return nil, nil
	}
	return txToRPC(tx), nil
}

func (h *Handler) rpcGetBlockTxCount(params json.RawMessage) (interface{}, *jsonRPCError) {
	var args []interface{}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &args)
	}
	if len(args) < 1 {
		return nil, &jsonRPCError{Code: -32602, Message: "block number required"}
	}
	num, err := h.parseBlockNumberArg(args[0])
	if err != nil {
		return nil, &jsonRPCError{Code: -32602, Message: err.Error()}
	}
	block := h.blockchain.GetBlockByNumber(num)
	if block == nil {
		return "0x0", nil
	}
	return fmt.Sprintf("0x%x", len(block.TxHashes)), nil
}

func (h *Handler) parseBlockNumberArg(v interface{}) (int64, error) {
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
			n, err := strconv.ParseInt(s[2:], 16, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid hex block number")
			}
			return n, nil
		}
		if s == "latest" {
			return h.blockchain.HeadBlockNumber(), nil
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid block number")
		}
		return n, nil
	case float64:
		return int64(t), nil
	default:
		return 0, fmt.Errorf("invalid block number type")
	}
}

func blockToRPC(b *blockchain.BlockRecord) map[string]interface{} {
	txs := make([]interface{}, 0, len(b.TxHashes))
	for _, h := range b.TxHashes {
		txs = append(txs, h)
	}
	return map[string]interface{}{
		"number":           fmt.Sprintf("0x%x", b.BlockNumber),
		"hash":             b.BlockHash,
		"parentHash":       b.PreviousHash,
		"timestamp":        fmt.Sprintf("0x%x", b.Timestamp),
		"transactions":     txs,
		"transactionCount": len(b.TxHashes),
		"merkleRoot":       b.MerkleRoot,
		"stateRoot":        b.StateRoot,
		"totalFees":        b.TotalFees,
		"producerNodeId":   b.ProducerNodeID,
	}
}

func txToRPC(tx *blockchain.Transaction) map[string]interface{} {
	return map[string]interface{}{
		"hash":  tx.Hash,
		"from":  tx.From,
		"to":    tx.To,
		"value": tx.Value,
		"fee":   tx.Fee,
		"nonce": fmt.Sprintf("0x%x", tx.Nonce),
		"asset": tx.Asset,
	}
}
