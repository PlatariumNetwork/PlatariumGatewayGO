# PlatariumGateway (Go Version)

Gateway for blockchain integration via REST API and WebSocket in the Platarium network.  
Full implementation in Go with P2P synchronization support between nodes.

## Architecture: Go as mediator, connection to Core

**Only the Gateway (Go) talks to Core.** Clients and other nodes do not communicate with Core directly.

- **Clients / network** → send requests and transactions to **Gateway (Go)**.
- **Gateway (Go)** → the only component that **connects to Platarium Core** (Rust): it calls `platarium-cli` for signature verification, key generation, etc.
- **Core (Rust)** — cryptography and protocol; it has no server of its own and is used only by the Gateway.

In testnet mode the Gateway decides when to call Core to validate TX; clients simply submit TX to the REST/WS Gateway.

**Decentralized network:** it does not matter which node receives a transaction — it enters the shared Mempool and is synchronized across nodes. Nodes take TX from the Mempool; the reputation-based protocol (selection weight) chooses L1 (block collection) and L2 (confirmation).

## Features

- **P2P Network Synchronization** - Multiple nodes automatically connect and work as a synchronized network
- REST API for querying balances, transactions, and sending new transactions
- WebSocket server for real-time transaction broadcasting
- **Dynamic node discovery** - Nodes automatically discover and connect to peer nodes
- **Real-time event broadcasting** - Blockchain events are synchronized across all connected nodes
- **Global socket registry** - Track all connected clients across all nodes
- Modular structure for easy extension

## Requirements

- Go 1.21 or newer

## Installation

```bash
cd PlatariumGatewayGO
go mod download
go build -o platarium-gateway
```

## Usage

### Starting a Single Node

```bash
./platarium-gateway
```

Or with parameters:

```bash
./platarium-gateway --port 1812 --ws 1813
```

This will run:
- REST API at `http://localhost:1812`
- WebSocket server at `ws://localhost:1813`

### Test Network (Core validation)

To run a **test network** that validates every transaction with **Platarium Core** (Rust):

1. Build Core: `cd PlatariumCore && cargo build --release`
2. Start the gateway in testnet mode (Core is **required**; gateway exits if `platarium-cli` is not found):

```bash
./platarium-gateway -testnet -port 2812 -ws 2813
```

In testnet mode:
- Every submitted transaction **must** include a valid **signature** and **pubkey** (or **from** as public key).
- The gateway calls `platarium-cli verify-signature` to validate; invalid or missing signature returns `400 Bad Request`.
- This verifies that transactions are validated correctly by the Core before being accepted and distributed.

**Run the full test suite (Core unit tests + optional integration tests):**

```bash
# From repo root or PlatariumGatewayGO
chmod +x PlatariumGatewayGO/scripts/run_testnet.sh
./PlatariumGatewayGO/scripts/run_testnet.sh
```

Or manually:
```bash
cd PlatariumGatewayGO
go test -v ./internal/core/...          # Unit tests (sign/verify with Core)
./platarium-gateway -testnet -port 2812 -ws 2813 &   # Start testnet
go test -tags=integration -v -run TestTestnet .      # Integration tests (send TX)
```

### 100 RPC nodes + confirmation distribution test (two terminals)

Launch 100 test nodes with a single command and verify transaction propagation (1→2, 2→1) with statistics.

**Terminal 1 - start 100 nodes (testnet):**
```bash
cd PlatariumGatewayGO
chmod +x scripts/run_100nodes.sh
./scripts/run_100nodes.sh
```
Stop: `Ctrl+C` (stops all processes).

**Terminal 2 - send transactions and show stats:**
```bash
cd PlatariumGatewayGO
./scripts/send_tx_load_test.sh [ROUNDS]
```
- `ROUNDS` - number of transaction pairs (default 20): each round = one TX from account 1 to account 2 and one TX from account 2 to account 1.
- Outputs: OK/Fail per direction, total time, TX/s, and balance check on several nodes (propagation).

Before this, build Core: `cd PlatariumCore && cargo build --release`.

### Real mode: 3 nodes in 3 terminals

To work with a multi-node network (L1/L2 voting, peers), start **3 separate terminals**. Order matters: first node 0, then 1, then 2.

**Before first run (once):**
```bash
cd PlatariumGatewayGO
cd ../PlatariumCore && cargo build --release && cd ../PlatariumGatewayGO
go build -o platarium-gateway .
```

**Terminal 1 - node 0** (REST: 2812, WS: 2813, no peers):
```bash
cd PlatariumGatewayGO
./platarium-gateway -testnet -port 2812 -ws 2813
```

**Terminal 2 - node 1** (REST: 2822, WS: 2823, peer: node 0). Start after node 0 prints that it is listening on ports:
```bash
cd PlatariumGatewayGO
PEERS='["ws://localhost:2813"]' ./platarium-gateway -testnet -port 2822 -ws 2823
```

**Terminal 3 - node 2** (REST: 2832, WS: 2833, peers: node 0 and 1). Start after node 1:
```bash
cd PlatariumGatewayGO
PEERS='["ws://localhost:2813","ws://localhost:2823"]' ./platarium-gateway -testnet -port 2832 -ws 2833
```

After starting all three:
- API of node 0: `http://localhost:2812`
- API of node 1: `http://localhost:2822`
- API of node 2: `http://localhost:2832`

Open the visualization from any node (for example `http://localhost:2812/web/nodes-viz.html`) or switch `?api=2822` / `?api=2832` to view from other nodes. L1/L2 voting will gather votes from all peers.

You can also print these commands with the script: `./scripts/run_3nodes_windows.sh` (it only shows blocks of commands to copy).

### Diagnostics (logs)

All errors and key L1/L2 events (node selection, votes, forwarding, reputation) are written to the **`log/` folder**:
- **`log/gateway.log`** - one file per process (if you run several nodes from the same directory, they append to a single file; it is better to run each node from its own working directory or distinguish by time).
Levels: `INFO`, `WARN`, `ERROR`. Examples: who was selected for L1/L2, how many votes arrived, degraded mode, failed forwarding, reputation updates.

### L1/L2 node visualization (HTML)

Page with node squares, L1/L2 colors and voting statistics:

1. Start at least one node (for example `./platarium-gateway -testnet -port 2812 -ws 2813` or `./scripts/run_100nodes.sh`).
2. Open in browser: **http://localhost:2812/web/nodes-viz.html**

If 100 nodes are running and you see **ERR_NO_BUFFER_SPACE** or the page does not load - node 0 is overloaded (many peer connections). Open the visualization on another node, e.g. **http://localhost:2912/web/nodes-viz.html** or **http://localhost:3012/web/nodes-viz.html**. On the page you can disable “Refresh every 10s” to reduce load.

On the page:
- **Red squares** - nodes (list from `/network`).
- Button **“Refresh nodes”** - pulls the current node list from the network.
- Button **“Simulate TX (L1 → L2)”** - randomly selects L1 group (first layer) and L2 group (second layer), shows who voted “Yes” / “No”.
- **Bottom section** - statistics: how many in L1/L2, how many voted “Yes” / “No”, total TX confirmed.

### Starting Multiple Nodes

**Terminal 1 (Node 1):**
```bash
./platarium-gateway --port 1812 --ws 1813
```

**Terminal 2 (Node 2):**
```bash
./platarium-gateway --port 1822 --ws 1823
```

**Terminal 3 (Node 3):**
```bash
./platarium-gateway --port 1832 --ws 1833
```

### Configuring Peer Connections

#### Method 1: Using `peers.json`

Create or edit `peers.json`:

```json
{
  "peers": [
    "ws://localhost:1813",
    "ws://localhost:1823",
    "ws://192.168.0.15:1813"
  ]
}
```

#### Method 2: Using Environment Variables

```bash
export PEERS='["ws://localhost:1813","ws://localhost:1823"]'
export NODE_HOST=192.168.0.14
./platarium-gateway --port 1812 --ws 1813
```

### Command Line Arguments

- `--port <number>`: REST API port (default: 1812)
- `--ws <number>`: WebSocket server port (default: 1813)

## REST API Endpoints

### `GET /`

Health check and basic node information.

**Response:**
```json
{
  "message": "PlatariumGateway v1.0.0 is running (Go)",
  "nodeId": "072bd62f-7473-446f-a490-73dabd70b66a",
  "nodeAddress": "ws://192.168.0.134:1813",
  "connectedPeers": 3
}
```

### `GET /network`

Full network status including all connected peer nodes.

**Response:**
```json
{
  "nodeId": "072bd62f-7473-446f-a490-73dabd70b66a",
  "nodeAddress": "ws://192.168.0.134:1813",
  "connectedNodes": [
    {
      "nodeId": "a1b2c3d4-5678-90ab-cdef-123456789abc",
      "address": "ws://192.168.0.135:1813",
      "host": "192.168.0.135",
      "port": 1813
    }
  ]
}
```

### `GET /sockets`

Complete overview of all connected WebSocket clients across the entire network.

**Response:**
```json
{
  "nodeId": "072bd62f-7473-446f-a490-73dabd70b66a",
  "nodeAddress": "ws://192.168.0.134:1813",
  "connectedSockets": [
    {
      "socketId": "51rahc7PzglDvO7WAAAB",
      "ipAddress": "::ffff:127.0.0.1",
      "connectedAt": "2025-11-01T14:22:33.511Z",
      "nodeId": "072bd62f-7473-446f-a490-73dabd70b66a"
    }
  ],
  "summary": {
    "connectedClients": 3,
    "connectedPeers": 2
  }
}
```

### `GET /pg-bal/{address}`

Get balance of an address.

### `GET /pg-tx/{hash}`

Get transaction by hash.

### `GET /pg-alltx/{address}`

Get all transactions for an address.

### `POST /pg-sendtx`

Send a new signed transaction. Transaction events are automatically broadcast to all peer nodes.

## Logging

The server logs important events with structured prefixes:

### Node Events
- `[NODE] Initialized node: <nodeId> at <address>` - Node startup
- `[NODE] Connected: <nodeId>... at <address>` - Peer node connected
- `[NODE] Disconnected: <nodeId>...` - Peer node disconnected
- `[NODE] Broadcasted <eventType> to <count> node(s)` - Event broadcasted

### Socket Events
- `[SOCKET] Client connected: <id> (IP: <ip>)` - Local client connected
- `[SOCKET] Client disconnected: <id>` - Local client disconnected
- `[WS] New client connected on <nodeId>... (IP: <ip>)` - Client connected on peer node
- `[WS] Client disconnected on <nodeId>...` - Client disconnected on peer node

### Transaction Events
- `Processing transaction: from <from> to <to> amount <amount>` - Transaction processing
- `Transaction added with hash: <hash>, Fee: <fee> coreon` - Transaction completed
- `Transaction error: <error>` - Transaction error

## Testing

### Test Multi-Node Setup

1. **Start Node 1:**
   ```bash
   ./platarium-gateway --port 1812 --ws 1813
   ```

2. **Start Node 2** (in another terminal):
   ```bash
   # Update peers.json to include Node 1
   ./platarium-gateway --port 1822 --ws 1823
   ```

3. **Verify Connection:**
   - Check logs for: `[NODE] Connected: ...`
   - Call `GET http://localhost:1812/network` - should show Node 2
   - Call `GET http://localhost:1822/network` - should show Node 1

## Architecture

### P2P Design Principles

- **No Central Authority**: All nodes operate independently
- **Equal Hierarchy**: No master/slave relationships
- **Symmetric Communication**: Each node is both server and client
- **Fault Tolerance**: Network continues to function even if some nodes go offline
- **Eventual Consistency**: All nodes eventually see the same blockchain state

## License

MIT License © Platarium Network
