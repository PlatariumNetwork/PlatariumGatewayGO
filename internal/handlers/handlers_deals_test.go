package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"platarium-gateway-go/internal/blockchain"
	"platarium-gateway-go/internal/core"
	"platarium-gateway-go/internal/deals"
	"platarium-gateway-go/internal/escrow"

	"github.com/gorilla/mux"
)

func TestBalanceKeepsSpendableWhenReceivableChanges(t *testing.T) {
	first := balancePayload("Px", &core.AccountQuery{Balance: "9300", LockedUplp: "0", ReceivableUplp: "0"}, map[string]string{}, "0")
	second := balancePayload("Px", &core.AccountQuery{Balance: "9300", LockedUplp: "0", ReceivableUplp: "250"}, map[string]string{}, "0")
	if first["balance"] != "9300" || second["balance"] != first["balance"] {
		t.Fatalf("balance changed %v %v", first["balance"], second["balance"])
	}
	if second["receivable_uplp"] != "250" || second["locked_uplp"] != "0" {
		t.Fatalf("%v", second)
	}
}

func TestListedNativeAssetIsPLP(t *testing.T) {
	native := presentListedTx(&blockchain.Transaction{Asset: "", AssetType: "native"})
	if native.Asset != "PLP" || native.AssetType != "native" {
		t.Fatalf("%+v", native)
	}
	named := presentListedTx(&blockchain.Transaction{Asset: "NATIVE", AssetType: "native"})
	if named.Asset != "PLP" {
		t.Fatalf("%+v", named)
	}
	token := presentListedTx(&blockchain.Transaction{Asset: "Token:USDC", AssetType: "token"})
	if token.Asset != "Token:USDC" {
		t.Fatalf("%+v", token)
	}
}

func TestTransactionsDefaultIsConfirmed(t *testing.T) {
	if !transactionsUseConfirmedDefault(url.Values{}) {
		t.Fatal("default")
	}
	if transactionsUseConfirmedDefault(url.Values{"mempool": []string{"1"}}) {
		t.Fatal("mempool flag")
	}
	if transactionsUseConfirmedDefault(url.Values{"pending": []string{"1"}}) {
		t.Fatal("pending flag")
	}
}

func dealTxJSON(kind, purpose, asset, node, outcome string) string {
	payload := map[string]interface{}{
		"hash": "h-" + kind + "-" + purpose + "-" + asset + "-" + node + "-" + outcome,
		"from": "payer", "to": "payee", "tx_kind": kind, "asset": asset,
		"amount": 100, "fee_uplp": 1, "nonce": 1, "escrow_id": "escrow-1",
		"purpose": purpose, "settle_node": node, "settle_outcome_key": outcome,
		"sig_main": "sig",
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

func TestDealLockAdmission(t *testing.T) {
	h := &Handler{blockchain: blockchain.NewBlockchain()}
	prev := signedDealAdmit
	called := 0
	signedDealAdmit = func(h *Handler, tx *blockchain.Transaction) error {
		called++
		return h.blockchain.AddToMempool(tx)
	}
	t.Cleanup(func() { signedDealAdmit = prev })

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/deals/lock", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		h.SubmitDealLock(w, req)
		return w
	}
	if w := post(`{"mnemonic":"abandon abandon","transaction":{}}`); w.Code != http.StatusBadRequest || called != 0 {
		t.Fatalf("mnemonic status %d calls %d", w.Code, called)
	}
	raw := dealTxJSON(escrow.TxKindLock, escrow.PurposeDeal, "PLP", "", "")
	if w := post(`{"transaction":` + raw + `}`); w.Code != http.StatusBadRequest {
		t.Fatalf("missing node %d %s", w.Code, w.Body.String())
	}
	raw = dealTxJSON(escrow.TxKindLock, escrow.PurposeDeal, "Token:USDC", "node-1", "")
	if w := post(`{"transaction":` + raw + `}`); w.Code != http.StatusBadRequest {
		t.Fatalf("asset %d", w.Code)
	}
	h.operatorWallet = "node-1"
	raw = dealTxJSON(escrow.TxKindLock, escrow.PurposeDeal, "PLP", "other-node", "")
	if w := post(`{"transaction":` + raw + `}`); w.Code != http.StatusBadRequest {
		t.Fatalf("foreign guarantor %d %s", w.Code, w.Body.String())
	}
	raw = dealTxJSON(escrow.TxKindLock, escrow.PurposeDeal, "PLP", "node-1", "")
	w := post(`{"transaction":` + raw + `}`)
	if w.Code != http.StatusOK {
		t.Fatalf("admit %d %s", w.Code, w.Body.String())
	}
	if len(h.blockchain.GetMempool()) != 1 || h.blockchain.GetMempool()[0].Purpose != escrow.PurposeDeal {
		t.Fatalf("mempool %+v", h.blockchain.GetMempool())
	}
}

func TestDealSettleDoesNotRewriteOutcome(t *testing.T) {
	h := &Handler{blockchain: blockchain.NewBlockchain()}
	prev := signedDealAdmit
	var got *blockchain.Transaction
	signedDealAdmit = func(h *Handler, tx *blockchain.Transaction) error {
		got = tx
		return h.blockchain.AddToMempool(tx)
	}
	t.Cleanup(func() { signedDealAdmit = prev })
	raw := dealTxJSON(escrow.TxKindSettle, escrow.PurposeDeal, "PLP", "node-1", "accept")
	req := httptest.NewRequest(http.MethodPost, "/api/deals/settle", bytes.NewBufferString(`{"transaction":`+raw+`}`))
	w := httptest.NewRecorder()
	h.SubmitDealSettle(w, req)
	if w.Code != http.StatusOK || got == nil || got.SettleOutcomeKey != "accept" || got.AmountUplp != 100 {
		t.Fatalf("status %d tx %+v body %s", w.Code, got, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/deals/settle", bytes.NewBufferString(`{"mnemonic":"x","transaction":`+raw+`}`))
	w = httptest.NewRecorder()
	h.SubmitDealSettle(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("mnemonic settle %d", w.Code)
	}
}

func TestDealReadFromCoreFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	body := `{"escrows_json":["{\"escrow_id\":\"deal-1\",\"creator\":\"alice\",\"beneficiary\":\"bob\",\"node\":\"n\",\"amount\":10,\"peaceful_fee\":1,\"reserved_fee\":2,\"asset\":\"PLP\",\"purpose\":\"deal\",\"status\":\"Locked\",\"expires_at\":9}","{\"escrow_id\":\"contact-1\",\"creator\":\"alice\",\"beneficiary\":\"bob\",\"purpose\":\"contact\",\"status\":\"Locked\",\"asset\":\"PLP\",\"amount\":3}"]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &Handler{dealStateFile: path}
	req := httptest.NewRequest(http.MethodGet, "/api/deals?address=bob", nil)
	w := httptest.NewRecorder()
	h.ListDeals(w, req)
	if w.Code != http.StatusOK || bytes.Contains(w.Body.Bytes(), []byte("contact-1")) == false && bytes.Contains(w.Body.Bytes(), []byte("deal-1")) == false {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("contact-1")) {
		t.Fatal("contact id listed")
	}
	req = httptest.NewRequest(http.MethodGet, "/api/deals/deal-1", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "deal-1"})
	w = httptest.NewRecorder()
	h.GetDeal(w, req)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`"peaceful_fee":1`)) {
		t.Fatalf("get %d %s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/deals/nope", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "nope"})
	w = httptest.NewRecorder()
	h.GetDeal(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing %d", w.Code)
	}
}

func TestVolunteerBriefOmitsParties(t *testing.T) {
	h := &Handler{}
	openBody := `{"terms":"deliver","openBlock":"4","escrowId":"escrow-7","bonded":["creator","beneficiary","node","v1","v2","v3","v4","v5"],"creator":"creator","beneficiary":"beneficiary","node":"node"}`
	req := httptest.NewRequest(http.MethodPost, "/api/deals/dispute", bytes.NewBufferString(openBody))
	w := httptest.NewRecorder()
	h.OpenDealDispute(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("open %d %s", w.Code, w.Body.String())
	}
	var brief deals.Brief
	if err := json.Unmarshal(w.Body.Bytes(), &brief); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/deals/cases/"+brief.CaseID, nil)
	req = mux.SetURLVars(req, map[string]string{"caseId": brief.CaseID})
	w = httptest.NewRecorder()
	h.GetVolunteerBrief(w, req)
	var raw map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 3 || raw["caseId"] == nil || raw["terms"] == nil || raw["termsHash"] == nil {
		t.Fatalf("keys %v", raw)
	}
	for _, banned := range []string{"creator", "beneficiary", "node", "amount", "asset", "escrowId", "escrow_id"} {
		if _, ok := raw[banned]; ok {
			t.Fatalf("leaked %s", banned)
		}
	}
}

func TestDisputeSettleNeedsThree(t *testing.T) {
	h := &Handler{blockchain: blockchain.NewBlockchain()}
	bonded := []string{"c", "b", "n", "v1", "v2", "v3", "v4", "v5"}
	brief, err := h.deals().Open("terms", "1", "escrow-1", bonded, []string{"c", "b", "n"})
	if err != nil {
		t.Fatal(err)
	}
	five, ok := h.deals().Panel(brief.CaseID)
	if !ok {
		t.Fatal("panel")
	}
	prev := signedDealAdmit
	admits := 0
	signedDealAdmit = func(h *Handler, tx *blockchain.Transaction) error {
		admits++
		return h.blockchain.AddToMempool(tx)
	}
	t.Cleanup(func() { signedDealAdmit = prev })
	for i := 0; i < 2; i++ {
		if err := h.deals().Commit(brief.CaseID, five[i], deals.HashVerdict(deals.VerdictFulfilled)); err != nil {
			t.Fatal(err)
		}
		if err := h.deals().Reveal(brief.CaseID, five[i], deals.VerdictFulfilled); err != nil {
			t.Fatal(err)
		}
	}
	raw := dealTxJSON(escrow.TxKindSettle, escrow.PurposeDeal, "PLP", "n", deals.OutcomeFulfilled)
	body := `{"caseId":"` + brief.CaseID + `","transaction":` + raw + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/deals/dispute-settle", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.SubmitDealDisputeSettle(w, req)
	if w.Code != http.StatusBadRequest || admits != 0 {
		t.Fatalf("two reveals %d admits %d %s", w.Code, admits, w.Body.String())
	}
	if err := h.deals().Commit(brief.CaseID, five[2], deals.HashVerdict(deals.VerdictFulfilled)); err != nil {
		t.Fatal(err)
	}
	if err := h.deals().Reveal(brief.CaseID, five[2], deals.VerdictFulfilled); err != nil {
		t.Fatal(err)
	}
	h.deals().SetHeight(2)
	req = httptest.NewRequest(http.MethodPost, "/api/deals/dispute-settle", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	h.SubmitDealDisputeSettle(w, req)
	if w.Code != http.StatusOK || admits != 1 {
		t.Fatalf("three %d admits %d %s", w.Code, admits, w.Body.String())
	}
}
