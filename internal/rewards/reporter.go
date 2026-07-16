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

// ReportNodeReward sends an approved XP credit to the Contributor Program API.
func ReportNodeReward(apiBase, secret string, report NodeRewardReport) error {
	if apiBase == "" || secret == "" {
		return fmt.Errorf("contributors API URL or secret not configured")
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
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("X-Platarium-Node-Rewards-Secret", secret)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("node-rewards HTTP %d: %s", resp.StatusCode, stringsTrim(string(raw)))
	}
	return nil
}

func stringsTrim(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
