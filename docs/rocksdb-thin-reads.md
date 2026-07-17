# Gateway thin reads against Core RocksDB

Canonical chain data (transactions, blocks, accounts, receipts) lives in **PlatariumCore RocksDB**, not in Gateway JSON files.

## What is no longer canonical

| Legacy file | Role now |
|-------------|----------|
| `core-state.json` (`PLATARIUM_STATE_FILE`) | Still used for mempool admission and `state-apply-tx` during block confirmation; **not** the explorer source of truth for confirmed balances when RocksDB is populated |
| `*-chain.json` (`PLATARIUM_CHAIN_FILE`) | Legacy snapshot for migration and in-memory vote metadata; **not** written when RocksDB is enabled |

Migrate existing JSON into RocksDB once:

```bash
platarium-cli migrate-json-to-rocks \
  --db-path ./data/rocksdb \
  --chain-file ./data/core-state-chain.json
```

Gateway runs this automatically on startup when RocksDB is empty and a chain file exists.

## Environment

| Variable | Description |
|----------|-------------|
| `PLATARIUM_ROCKSDB_PATH` | RocksDB directory (default: `{dirname(PLATARIUM_STATE_FILE)}/rocksdb`) |
| `PLATARIUM_CLI_PATH` | Path to `platarium-cli` (passes `--db-path` on every rocks call) |
| `PLATARIUM_CORE_MODE` | `cli` (default) or `rpc` |

## Thin reads (Gateway → Core)

When RocksDB is available, handlers query Core via `internal/core/rocks_store.go`:

- `GET /pg-bal/{address}` → `rocks_get_account` (fallback: `state-query`)
- `GET /pg-tx/{hash}` → `rocks_get_tx` (fallback: in-memory index; mempool stays in RAM)
- `GET /pg-alltx/{address}` → `rocks_list_address_txs` + mempool/pending
- `GET /api/transactions`, `/api/blocks`, `/api/block/{n}` → rocks block/tx iteration
- `POST /rpc/v1` chain methods → same blockchain read path

## Block commit (L2 finality)

On L2 confirmation, after `assemble-block`:

1. Gateway builds a Core `BlockCommit` payload (block header, tx JSONs, updated accounts, receipts).
2. Calls `rocks_commit_block` via CLI/RPC.
3. `PersistChainSnapshot` / `*-chain.json` writes are **no-ops** when RocksDB is enabled.

Only the **L2 confirmer** node should commit blocks (single RocksDB writer per path).

## Mempool

Mempool and pending blocks remain in Gateway RAM only (Core design). Clients rebroadcast after restart.
