package blockchain

import (
	"encoding/json"
	"fmt"
	"os"

	"platarium-gateway-go/internal/core"
)

// ConsistencyStatus values for Gateway diagnostic output (issue #67).
const (
	ConsistencyStatusConsistent = "CONSISTENT"
	ConsistencyStatusDiverged   = "DIVERGED"
)

// LayerTip is a height/hash view of one store layer.
type LayerTip struct {
	Path      string `json:"path,omitempty"`
	Height    uint64 `json:"height"`
	BlockHash string `json:"block_hash,omitempty"`
	StateRoot string `json:"state_root,omitempty"`
	Present   bool   `json:"present"`
	Error     string `json:"error,omitempty"`
}

// ConsistencyDiagnostic is a read-only multi-layer tip comparison.
// Diagnostic only — never repairs ledgers (see ADRConsistencyDiagnosticOnly).
type ConsistencyDiagnostic struct {
	Status         string   `json:"status"`
	DiagnosticOnly bool     `json:"diagnostic_only"`
	Note           string   `json:"note"`
	StateFile      LayerTip `json:"state_file"`
	Rocks          LayerTip `json:"rocks"`
	ChainJSON      LayerTip `json:"chain_json"`
	CoreOK         *bool    `json:"core_ok,omitempty"`
	CoreHead       *uint64  `json:"core_head,omitempty"`
	Reasons        []string `json:"reasons,omitempty"`
}

// ADRConsistencyDiagnosticOnly documents that GET /internal/consistency and --doctor
// are diagnostics only and must not mutate or auto-repair ledgers (issue #67).
const ADRConsistencyDiagnosticOnly = `Consistency diagnostic ≠ repair.

GET /internal/consistency and --doctor report CONSISTENT | DIVERGED for
state_file, Rocks, and chain.json tip height/hash views. Prefer Core
check_consistency when available. This path never writes ledgers, never
deletes backups, and never runs migrate/repair. Operators must repair
explicitly via separate tooling after reviewing the report.
`

// BuildConsistencyDiagnostic compares state_file / Rocks / chain.json tips (read-only).
func (bc *Blockchain) BuildConsistencyDiagnostic() ConsistencyDiagnostic {
	report := ConsistencyDiagnostic{
		DiagnosticOnly: true,
		Note:           "Diagnostic only — does not repair ledgers. " + ADRConsistencyDiagnosticOnly,
		Status:         ConsistencyStatusConsistent,
		Reasons:        nil,
	}

	// --- state_file ---
	bc.mu.RLock()
	ledger := bc.ledger
	chainPath := bc.chainFile
	history := append([]BlockRecord(nil), bc.blockHistory...)
	bc.mu.RUnlock()

	if ledger != nil {
		report.StateFile.Path = ledger.StateFilePath()
		report.StateFile.Present = true
		if root, err := ledger.StateRoot(); err != nil {
			report.StateFile.Error = err.Error()
			report.Reasons = append(report.Reasons, "state_file state_root: "+err.Error())
		} else {
			report.StateFile.StateRoot = root
		}
	} else {
		report.StateFile.Present = false
		report.StateFile.Error = "ledger unavailable"
	}

	// --- Rocks (prefer Core check_consistency) ---
	rocks := bc.rocksClient()
	if rocks != nil && rocks.Enabled() {
		report.Rocks.Path = rocks.DBPath()
		report.Rocks.Present = true
		if coreRep, err := rocks.CheckConsistency(); err != nil {
			report.Rocks.Error = err.Error()
			report.Reasons = append(report.Reasons, "core check_consistency: "+err.Error())
		} else {
			ok := coreRep.OK
			report.CoreOK = &ok
			h := coreRep.Head
			report.CoreHead = &h
			report.Rocks.Height = coreRep.Head
			if !coreRep.OK {
				for _, e := range coreRep.Errors {
					report.Reasons = append(report.Reasons, "rocks_internal: "+e)
				}
			}
		}
		if head, err := rocks.RocksGetHead(); err == nil {
			report.Rocks.Height = head
			if head > 0 {
				if found, blk, berr := rocks.RocksGetBlock(head); berr == nil && found && blk != nil {
					report.Rocks.BlockHash = blk.BlockHash
					report.Rocks.StateRoot = blk.StateRoot
				}
			}
		} else if report.Rocks.Error == "" {
			report.Rocks.Error = err.Error()
		}
	} else {
		report.Rocks.Present = false
		report.Rocks.Error = "rocks unavailable"
	}

	// --- chain.json / explorer tip ---
	report.ChainJSON.Path = chainPath
	if chainPath != "" {
		if _, err := os.Stat(chainPath); err == nil {
			report.ChainJSON.Present = true
		}
	}
	if len(history) > 0 {
		tip := history[len(history)-1]
		report.ChainJSON.Present = true
		report.ChainJSON.Height = core.GatewayBlockToRocksHeight(tip.BlockNumber)
		report.ChainJSON.BlockHash = tip.BlockHash
		report.ChainJSON.StateRoot = tip.StateRoot
	} else if chainPath != "" {
		// Try on-disk file if memory empty.
		if raw, err := os.ReadFile(chainPath); err == nil {
			var file ChainFileData
			if json.Unmarshal(raw, &file) == nil && len(file.Blocks) > 0 {
				tip := file.Blocks[len(file.Blocks)-1]
				report.ChainJSON.Present = true
				report.ChainJSON.Height = core.GatewayBlockToRocksHeight(tip.BlockNumber)
				report.ChainJSON.BlockHash = tip.BlockHash
				report.ChainJSON.StateRoot = tip.StateRoot
			}
		}
	}

	// --- Compare layers ---
	reasons := EvaluateLayerConsistency(report.Rocks, report.ChainJSON, report.StateFile, report.CoreOK)
	report.Reasons = append(report.Reasons, reasons...)

	if len(report.Reasons) > 0 {
		report.Status = ConsistencyStatusDiverged
	} else {
		report.Status = ConsistencyStatusConsistent
	}
	return report
}

// EvaluateLayerConsistency compares Rocks / chain.json / state_file tips (#68 fixtures).
// Returns mismatch reasons; empty means CONSISTENT.
func EvaluateLayerConsistency(rocks, chainJSON, stateFile LayerTip, coreOK *bool) []string {
	var reasons []string
	if rocks.Present && chainJSON.Present && chainJSON.Height > 0 {
		if rocks.Height != chainJSON.Height {
			reasons = append(reasons, fmt.Sprintf(
				"height_mismatch: rocks=%d chain_json=%d", rocks.Height, chainJSON.Height))
		}
		if rocks.BlockHash != "" && chainJSON.BlockHash != "" &&
			rocks.BlockHash != chainJSON.BlockHash {
			reasons = append(reasons, fmt.Sprintf(
				"hash_mismatch: rocks=%s chain_json=%s", rocks.BlockHash, chainJSON.BlockHash))
		}
		if rocks.StateRoot != "" && chainJSON.StateRoot != "" &&
			rocks.StateRoot != chainJSON.StateRoot {
			reasons = append(reasons, fmt.Sprintf(
				"state_root_mismatch: rocks=%s chain_json=%s", rocks.StateRoot, chainJSON.StateRoot))
		}
	}
	if rocks.Present && stateFile.Present &&
		rocks.StateRoot != "" && stateFile.StateRoot != "" &&
		rocks.StateRoot != stateFile.StateRoot {
		reasons = append(reasons, fmt.Sprintf(
			"state_file_vs_rocks_root: state_file=%s rocks=%s", stateFile.StateRoot, rocks.StateRoot))
	}
	if coreOK != nil && !*coreOK {
		// Caller already recorded rocks_internal reasons when Core reported !OK.
		_ = coreOK
	}
	return reasons
}

// StatusFromReasons maps reason list to CONSISTENT | DIVERGED (#68).
func StatusFromReasons(reasons []string) string {
	if len(reasons) > 0 {
		return ConsistencyStatusDiverged
	}
	return ConsistencyStatusConsistent
}
