# PlatariumGateway (Go Version)

Gateway for blockchain integration via REST API and WebSocket in the Platarium network.  
Full implementation in Go with P2P synchronization support between nodes.

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
