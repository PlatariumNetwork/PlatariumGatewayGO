# Deployment Guide for Platarium Gateway

This guide explains how to deploy Platarium Gateway with HTTPS support using Nginx.

## Prerequisites

- Go 1.21 or newer
- Nginx installed and configured
- SSL certificates for your domain
- Systemd (for service management)

## Step 1: Build the Application

```bash
cd PlatariumGatewayGO
go mod download
go build -o platarium-gateway
```

## Step 2: Create Systemd Service

Create `/etc/systemd/system/platarium-gateway.service`:

```ini
[Unit]
Description=Platarium Gateway RPC Server
After=network.target

[Service]
Type=simple
User=www-data
WorkingDirectory=/path/to/PlatariumGatewayGO
ExecStart=/path/to/PlatariumGatewayGO/platarium-gateway --port 1812 --ws 1813
Restart=always
RestartSec=5
Environment="NODE_HOST=31.172.71.182"

[Install]
WantedBy=multi-user.target
```

Enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable platarium-gateway
sudo systemctl start platarium-gateway
sudo systemctl status platarium-gateway
```

## Step 3: Configure Nginx

1. Copy the nginx configuration:

```bash
sudo cp nginx.conf /etc/nginx/sites-available/platarium-gateway
sudo ln -s /etc/nginx/sites-available/platarium-gateway /etc/nginx/sites-enabled/
```

2. Update SSL certificate paths in the config:

Edit `/etc/nginx/sites-available/platarium-gateway` and update:
- `ssl_certificate` path
- `ssl_certificate_key` path

3. Test and reload Nginx:

```bash
sudo nginx -t
sudo systemctl reload nginx
```

## Step 4: Configure Firewall

Allow necessary ports:

```bash
# HTTP and HTTPS
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp

# Internal ports (only if needed for direct access)
sudo ufw allow 1812/tcp
sudo ufw allow 1813/tcp
```

## Step 5: Verify Deployment

1. Check service status:
```bash
sudo systemctl status platarium-gateway
```

2. Check Nginx status:
```bash
sudo systemctl status nginx
```

3. Test the endpoints:
```bash
# Test status endpoint
curl https://rpc-melancholy-testnet.platarium.network/rpc/status

# Test web interface
curl https://rpc-melancholy-testnet.platarium.network/
```

## Step 6: Configure Peers

Update `peers.json` with actual peer addresses:

```json
{
  "peers": [
    "ws://peer1.platarium.network:1813",
    "ws://peer2.platarium.network:1813"
  ]
}
```

Or use environment variable:

```bash
export PEERS='["ws://peer1.platarium.network:1813"]'
```

## Monitoring

View logs:

```bash
# Application logs
sudo journalctl -u platarium-gateway -f

# Nginx access logs
sudo tail -f /var/log/nginx/platarium-gateway-access.log

# Nginx error logs
sudo tail -f /var/log/nginx/platarium-gateway-error.log
```

## Troubleshooting

### Service won't start
- Check logs: `sudo journalctl -u platarium-gateway -n 50`
- Verify binary path and permissions
- Check if ports 1812 and 1813 are available

### Nginx 502 Bad Gateway
- Verify Go service is running: `sudo systemctl status platarium-gateway`
- Check Nginx error logs: `sudo tail -f /var/log/nginx/error.log`
- Verify proxy_pass URL is correct

### SSL Certificate Issues
- Verify certificate paths in nginx config
- Check certificate validity: `openssl x509 -in /path/to/cert.crt -text -noout`
- Ensure certificate includes the domain name

### WebSocket Not Working
- Verify WebSocket endpoint is accessible
- Check Nginx WebSocket configuration
- Test direct connection: `wscat -c ws://127.0.0.1:1813`

## Security Considerations

1. **Firewall**: Only expose ports 80 and 443 publicly
2. **SSL**: Use strong SSL/TLS configuration
3. **Updates**: Keep Go, Nginx, and system packages updated
4. **Monitoring**: Set up log monitoring and alerts
5. **Backups**: Regularly backup configuration files

## Performance Tuning

For high-traffic deployments, consider:

1. **Nginx worker processes**: Adjust `worker_processes` in `/etc/nginx/nginx.conf`
2. **Connection limits**: Configure `worker_connections`
3. **Caching**: Add caching for static assets
4. **Load balancing**: Use multiple Go instances behind Nginx

