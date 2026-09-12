package handlers

import (
	"net/http"

	"platarium-gateway-go/internal/blockchain"
)

// ConsistencyCheck is GET /internal/consistency — read-only diagnostic (issue #67).
// Never mutates ledgers; diagnostic ≠ repair (see blockchain.ADRConsistencyDiagnosticOnly).
func (h *Handler) ConsistencyCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET required"})
		return
	}
	if h.blockchain == nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "blockchain unavailable"})
		return
	}
	report := h.blockchain.BuildConsistencyDiagnostic()
	status := http.StatusOK
	if report.Status == blockchain.ConsistencyStatusDiverged {
		status = http.StatusOK // still 200 — diagnostic, not an error channel
	}
	jsonResponse(w, status, report)
}

// RunDoctorConsistency returns the structured diagnostic for --doctor CLI.
func (h *Handler) RunDoctorConsistency() blockchain.ConsistencyDiagnostic {
	if h == nil || h.blockchain == nil {
		return blockchain.ConsistencyDiagnostic{
			Status:         blockchain.ConsistencyStatusDiverged,
			DiagnosticOnly: true,
			Note:           blockchain.ADRConsistencyDiagnosticOnly,
			Reasons:        []string{"blockchain unavailable"},
		}
	}
	return h.blockchain.BuildConsistencyDiagnostic()
}
