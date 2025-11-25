# Web Monitoring Interface

This directory contains the web-based monitoring interface for Platarium Gateway.

## Files

- `index.html` - Main monitoring dashboard

## Access

Once the server is running, access the monitoring interface at:

```
http://localhost:1812/
```

## Features

The monitoring interface displays:

1. **Component Status** - Real-time status of all system components:
   - REST API
   - WebSocket Server
   - P2P Connections
   - Balance System
   - Transactions Module

2. **Network Information**:
   - Network name
   - Node ID
   - Node Address
   - Connected Peers count

3. **Connected Clients & Peers**:
   - Total connected clients
   - Total connected peers
   - Detailed socket list with IP addresses and connection times

## API Endpoints Used

- `GET /rpc/status` - Detailed component status
- `GET /rpc/sockets` - Socket and peer information

## Auto-refresh

The interface automatically refreshes every 10 seconds to show the latest status.

