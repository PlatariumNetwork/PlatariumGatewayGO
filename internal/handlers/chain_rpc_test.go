package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"platarium-gateway-go/internal/blockchain"
)

func TestChainRPCBlockNumber(t *testing.T) {
	bc := blockchain.NewBlockchain()
	h := &Handler{blockchain: bc}

	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"platarium_blockNumber","params":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/rpc/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ChainRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("rpc error: %v", resp.Error)
	}
	if resp.Result != "0x0" {
		t.Fatalf("expected 0x0, got %v", resp.Result)
	}
}

func TestChainRPCUnknownMethod(t *testing.T) {
	bc := blockchain.NewBlockchain()
	h := &Handler{blockchain: bc}

	body := []byte(`{"jsonrpc":"2.0","id":2,"method":"platarium_unknown","params":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/rpc/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ChainRPC(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("expected method not found, got %+v", resp.Error)
	}
}
