package rewards

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// NodeRewardReport is POSTed to Scan after a node earns validation fees.
type NodeRewardReport struct {
	WalletAddress string `json:"walletAddress"`
	NodeID        string `json:"nodeId"`
	BlockNumber   uint64 `json:"blockNumber"`
	NetworkID     string `json:"networkId"`
	XP            int    `json:"xp"`
	FeeUplp       int64  `json:"feeUplp"`
	L1ShareUplp   int64  `json:"l1ShareUplp"`
	L2ShareUplp   int64  `json:"l2ShareUplp"`
	LoadPct       int    `json:"loadPct"`
	ReferenceID   string `json:"referenceId"`
	Reason        string `json:"reason"`
}

// ReportNodeReward sends a validation XP credit to the public Contributor Program API.
// No shared secret — any operator node may report; Scan enforces floor/cap and idempotency.
func ReportNodeReward(apiBase string, report NodeRewardReport) error {
	if apiBase == "" {
		return fmt.Errorf("contributors API URL not configured")
	}
	body, err := json.Marshal(report)
	if err != nil {
		return err
	}
	url := apiBase + "/api/contributors/node-rewards"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "PlatariumGateway/node-rewards")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := string(raw)
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		return fmt.Errorf("node-rewards HTTP %d: %s", resp.StatusCode, msg)
	}
	return nil
}
