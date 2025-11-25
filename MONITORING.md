# Monitoring Setup Guide

This guide explains how to set up the monitoring interface for Platarium Gateway.

## Features

The monitoring interface displays:

1. **Network Status**: ONLINE/OFFLINE indicator based on overall system status
2. **Component Status**: Real-time status of all system components:
   - REST API
   - WebSocket Server
   - P2P Connections
   - Balance System
   - Transactions Module

3. **Network Information**:
   - Network name
   - Node ID
   - Node Address
   - Connected Peers count

4. **Summary Panel**:
   - Total Connected Clients
   - Total Connected Peers

5. **Connected Peers List**:
   - Peer Node ID
   - Peer Address
   - Ping latency (ms) with color coding:
     - Green: < 50ms (good)
     - Orange: 50-150ms (medium)
     - Red: > 150ms (bad)
     - Gray: N/A (unknown)

6. **Connected Clients List**:
   - Socket ID
   - IP Address
   - Node where connected
   - Connection timestamp

## API Endpoints

### GET /rpc/status

Returns detailed status of all components:

```json
{
  "network": "Platarium",
  "status": "ok",
  "components": {
    "REST": "ok",
    "WebSocket": "ok",
    "P2P": "ok",
    "Balance": "ok",
    "Transactions": "ok"
  },
  "nodeId": "c9988c87-8cb2-4aac-8bd7-0c8d1c957778",
  "nodeAddress": "ws://31.172.71.182:1813",
  "connectedPeers": 2,
  "peers": [
    {
      "nodeId": "abc123...",
      "address": "ws://192.168.1.100:1813",
      "host": "192.168.1.100",
      "port": 1813,
      "ping": 45
    }
  ],
  "summary": {
    "connectedClients": 5,
    "connectedPeers": 2
  },
  "timestamp": 1764038873499
}
```

### GET /rpc/sockets

Returns list of all connected clients:

```json
{
  "nodeId": "...",
  "nodeAddress": "ws://...",
  "connectedSockets": [...],
  "summary": {
    "connectedClients": 5,
    "connectedPeers": 2
  }
}
```

### GET /rpc/ping?address=ws://...

Pings a specific peer address and returns latency.

## Accessing the Interface

### Local Development

```
http://localhost:1812/
```

### Production (via HTTPS)

```
https://rpc-melancholy-testnet.platarium.network/
```

## Auto-Refresh

The interface automatically refreshes every 10 seconds to show the latest status.

## Status Indicators

- **ONLINE** (Green): All critical components are operational
- **OFFLINE** (Red): One or more critical components are not operational

### Component Status Colors

- **OK** (Green): Component is working correctly
- **NOT OK** (Red): Component has an issue
- **Warning** (Orange): Component status is uncertain

## Testing

Test the status endpoint:

```bash
curl https://rpc-melancholy-testnet.platarium.network/rpc/status
```

Test the sockets endpoint:

```bash
curl https://rpc-melancholy-testnet.platarium.network/rpc/sockets
```

## Troubleshooting

### Interface shows OFFLINE

1. Check if the Go service is running:
   ```bash
   sudo systemctl status platarium-gateway
   ```

2. Check service logs:
   ```bash
   sudo journalctl -u platarium-gateway -n 50
   ```

3. Verify the endpoint is accessible:
   ```bash
   curl http://127.0.0.1:1812/rpc/status
   ```

### Ping shows N/A for all peers

- Peers might be unreachable
- Firewall might be blocking connections
- Peer nodes might be down
- Network latency might be too high (timeout)

### Components show NOT OK

- **REST**: Service might not be responding
- **WebSocket**: WebSocket server might not be initialized
- **P2P**: No peer nodes connected
- **Balance**: Blockchain module might not be initialized
- **Transactions**: Transaction module might have issues

## Performance Considerations

- Ping measurements are done concurrently but with a 2-second timeout
- Too many peers might slow down the status endpoint
- Consider caching ping results if needed
- Monitor API response times

