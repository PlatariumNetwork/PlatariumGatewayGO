// Package docs holds Gateway architecture decision records as Go source so they
// can merge to main under the orchestrator file policy (.go / .go.tmpl only).
//
// ADR index:
//   - Canonical vs rebuildable stores: see ADRCanonicalVsRebuildableStores (issue #55 / TASK-085).
package docs

// ADRCanonicalVsRebuildableStores is the Gateway ADR for store authority boundaries.
//
// # Decision
//
// RocksDB (via Core) is the canonical chain tip and account store. chain.json,
// explorer caches, and Core state_file JSON are rebuildable staging / restart
// caches — never a second source of truth.
//
// # What is canonical
//
//   - RocksDB committed tip height / tip hash after a successful Core finalize
//   - Account balances and nonces readable through Core Rocks-backed APIs
//   - Invariant: committed block B ↔ account/state after B (paired height + hash)
//
// # What is rebuildable / cache
//
//   - PLATARIUM_CHAIN_FILE (chain.json): explorer restart cache; rewritten from Rocks
//   - Explorer in-memory blockHistory / TX indexes: derived views
//   - PLATARIUM_STATE_FILE (core-state.json): staging / recovery cache, not dual SoT
//
// # Transaction boundary
//
// Gateway does not implement 2PC. Finality authority is Core finalize:
// prepare → execute → validate → persist → COMMITTED. Gateway may stage explorer
// views only after Core reports success; on Core failure Gateway must not advance
// a canonical tip (fail-closed).
//
// # Crash recovery
//
// After crash, restart rebuilds explorer/cache from Rocks. Tip leading Rocks head
// must not be served as canonical. Partial explorer writes without Core COMMITTED
// are discarded on recovery.
//
// # Flag matrix (TASK-015)
//
// Fail-closed defaults are documented in internal/handlers/lab_endpoints.go
// (ADR flag matrix). Summary:
//
//	Flag                              Default     Testnet intent                         Production intent
//	--------------------------------  ----------  -------------------------------------  --------------------------------
//	ENABLE_LAB_ENDPOINTS              unset/off   optional local labs only               must stay off
//	PLATARIUM_LAB_TOKEN               empty       required when lab on                   N/A (lab off)
//	PLATARIUM_CONSENSUS_INSECURE      false       local-only opt-in without token        must stay false
//	PLATARIUM_ALLOW_DEGRADED_CONSENSUS false      opt-in solo/dev proposer-only accept   must stay false
//
// Degraded consensus never overrides Core RPC failure (issue #53): uncertainty
// never increases authority.
//
// # Core finalize failure table (TASK-033)
//
// Explicit pointer: PlatariumCore finalize contract / failure table (TASK-033),
// typically under PlatariumCore docs ADR section covering phases
// PREPARE → execute → validate → persist → COMMITTED and the failure→result
// matrix (execution/JSON/Rocks/crash-before/crash-after/duplicate/conflict).
// Gateway aligns: any Core finalize or process-votes failure ⇒ no Gateway finality.
const ADRCanonicalVsRebuildableStores = `ADR: canonical vs rebuildable stores (Gateway)

Canonical: Core RocksDB tip + accounts after COMMITTED finalize.
Rebuildable: chain.json, explorer caches, state_file JSON staging.
Boundary: Core finalize phases; Gateway fail-closed on Core error.
Invariant: committed B ↔ state after B (height+hash pairing).
Crash recovery: rebuild explorer/cache from Rocks; do not serve tip leading Rocks;
partial explorer writes without Core COMMITTED are discarded/frozen on recovery.
Flag matrix: internal/handlers/lab_endpoints.go (TASK-015).
Core failure table: PlatariumCore TASK-033 finalize contract docs.
`
