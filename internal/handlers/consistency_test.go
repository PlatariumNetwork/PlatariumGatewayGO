package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"platarium-gateway-go/internal/blockchain"
)

func TestConsistencyCheckEndpointReadOnly(t *testing.T) {
	h := &Handler{blockchain: blockchain.NewBlockchain()}
	req := httptest.NewRequest(http.MethodGet, "/internal/consistency", nil)
	rr := httptest.NewRecorder()
	h.ConsistencyCheck(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["diagnostic_only"] != true {
		t.Fatalf("diagnostic_only: %v", body["diagnostic_only"])
	}
	if body["status"] != blockchain.ConsistencyStatusConsistent && body["status"] != blockchain.ConsistencyStatusDiverged {
		t.Fatalf("status: %v", body["status"])
	}
}

func TestConsistencyCheckRejectsPOST(t *testing.T) {
	h := &Handler{blockchain: blockchain.NewBlockchain()}
	req := httptest.NewRequest(http.MethodPost, "/internal/consistency", nil)
	rr := httptest.NewRecorder()
	h.ConsistencyCheck(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestRunDoctorConsistency(t *testing.T) {
	h := &Handler{blockchain: blockchain.NewBlockchain()}
	rep := h.RunDoctorConsistency()
	if !rep.DiagnosticOnly {
		t.Fatal("doctor must be diagnostic_only")
	}
}
