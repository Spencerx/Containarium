# Reverse Tunnel for Firewalled Spot Instances

The reverse tunnel allows a spot VM behind a firewall (no inbound connectivity) to join the Containarium sentinel architecture. The spot VM initiates an **outbound** TCP connection to the sentinel's public IP on port 443, multiplexed alongside regular HTTPS traffic.

## Problem

The standard sentinel architecture assumes the spot VM is in the same VPC as the sentinel, reachable via its private IP. This doesn't work when:

- The spot VM is behind a corporate firewall that blocks inbound connections
- The spot VM is on a different network or cloud provider
- The spot VM is behind NAT with no port forwarding

## Solution

The spot VM connects **outbound** to the sentinel. The sentinel multiplexes tunnel and HTTPS traffic on the same port (443) using first-byte protocol detection — no extra firewall port needed.

```
┌──────────────────────────────────────────────────────────────────┐
│                    Users (SSH / HTTP / HTTPS)                     │
└───────────────────────────┬──────────────────────────────────────┘
                            │
                            ▼
┌──────────────────────────────────────────────────────────────────┐
│              Sentinel VM (always-on, public IP)                   │
│              Port 443: ConnMux                                    │
│                ├─ first byte '{' → TunnelServer (yamux)           │
│                └─ first byte 0x16 → HTTPS (maintenance or proxy)  │
│              Port 22: sshpiper → tunnel → spot:22                 │
│              Port 80: iptables DNAT → tunnel → spot:80            │
└──────────────────────────────────────────────────────────────────┘
                            ▲
                            │ outbound TCP:443
                            │ (yamux-multiplexed)
                            │
┌──────────────────────────────────────────────────────────────────┐
│              Firewalled Spot VM                                   │
│              • No inbound ports needed                            │
│              • Runs `containarium tunnel` client                  │
│              • Local services: sshd:22, Caddy:443, daemon:8080    │
│              • All traffic proxied through yamux session           │
└──────────────────────────────────────────────────────────────────┘
```

## How It Works

### 1. Port Multiplexing (ConnMux)

The sentinel's ConnMux listens on port 443 and peeks the first byte of each incoming connection:

| First byte | Protocol | Routing |
|-----------|----------|---------|
| `{` (0x7B) | Tunnel handshake (JSON) | → TunnelServer |
| `0x16` | TLS ClientHello | → HTTPS (maintenance page or proxy to spot) |

This works because the tunnel handshake is a JSON object (`{"token":...}`), while TLS always starts with byte `0x16`. No extra port is needed.

### 2. Tunnel Handshake

When the spot connects to the sentinel on port 443:

```
Spot → Sentinel:  {"token":"SECRET","spot_id":"my-spot","ports":[22,80,443,8080]}
Sentinel → Spot:  {"ok":true,"assigned_ip":"127.0.0.2"}
```

After the handshake, the TCP connection is upgraded to a [yamux](https://github.com/hashicorp/yamux) multiplexed session.

### 3. Yamux Session

The yamux library multiplexes many logical streams over the single TCP connection:

- **Sentinel is the yamux client** (opens streams to "dial into" the spot)
- **Spot is the yamux server** (accepts streams and proxies to local ports)

When the sentinel needs to forward a connection to the spot (e.g., an HTTPS request), it opens a yamux stream, writes a 2-byte port header, then does bidirectional copy:

```
[2-byte port (big-endian)] [TCP data stream...]
```

The spot reads the port number, connects to `127.0.0.1:<port>` locally, and proxies the data.

### 4. Loopback Aliases

Each connected spot gets a loopback alias on the sentinel (e.g., `127.0.0.2`). The tunnel server opens TCP proxy listeners on `127.0.0.2:<port>` for each advertised port. This makes the tunneled spot look like a directly reachable IP to the existing sentinel code:

- Health checks dial `127.0.0.2:8080` → tunneled to spot's daemon
- sshpiper connects to `127.0.0.2:22` → tunneled to spot's sshd
- iptables DNAT for port 80 points to `127.0.0.2:80` → tunneled to spot's Caddy
- Key sync calls `http://127.0.0.2:8080/authorized-keys` → tunneled to spot

Port 443 is handled by the ConnMux directly (not iptables DNAT), since the mux needs to see every connection to distinguish tunnel from HTTPS.

### 5. Authentication

Phase 1 uses a pre-shared token. The spot includes the token in its JSON handshake. The sentinel validates it before accepting the connection.

The token is configured via `--tunnel-token` flag or `CONTAINARIUM_TUNNEL_TOKEN` environment variable on both sides.

## Usage

### Hybrid Mode (GCP + Tunnel) — Recommended for Production

Run both the GCP spot VM and firewalled bare metal simultaneously. Add `--tunnel-token` to your existing GCP sentinel command:

```bash
containarium sentinel \
  --spot-vm my-spot-vm --zone us-west1-a --project my-project \
  --tunnel-token SECRET \
  --forwarded-ports 80,443
```

This gives you:
- **GCP spot VM** as the primary backend (auto-restart on preemption)
- **Bare metal** as a secondary backend (connects via tunnel)
- **Automatic failover**: if GCP is preempted, HTTP switches to the tunnel backend
- **Per-user SSH routing**: sshpiper routes each user to whichever backend they belong to
- **Independent health checks**: each backend is monitored separately

### Pure Tunnel Mode (no GCP)

```bash
containarium sentinel \
  --provider=tunnel \
  --tunnel-token SECRET \
  --forwarded-ports 80,443
```

The sentinel starts in maintenance mode and waits for a spot to connect via the tunnel. When the spot connects and health checks pass, it switches to proxy mode.

### Spot VM Side

```bash
containarium tunnel \
  --sentinel-addr sentinel.example.com:443 \
  --token SECRET \
  --spot-id my-remote-spot \
  --ports 22,80,443,8080
```

The tunnel client:
1. Connects outbound to the sentinel's port 443
2. Authenticates with the pre-shared token
3. Establishes a yamux session
4. Accepts stream requests and proxies to local ports
5. Reconnects automatically with exponential backoff on disconnect

### Environment Variables

| Variable | Used by | Description |
|----------|---------|-------------|
| `CONTAINARIUM_TUNNEL_TOKEN` | Both | Pre-shared token (alternative to `--tunnel-token`/`--token`) |

## Data Flow Examples

### HTTPS Request (user → spot's web service)

```
User browser
  → sentinel:443 (TCP)
  → ConnMux peeks first byte: 0x16 (TLS) → HTTPS path
  → proxy to 127.0.0.2:443 (loopback alias)
  → TCP proxy listener on sentinel
  → yamux stream (port header: 443)
  → spot's tunnel client
  → 127.0.0.1:443 on spot (Caddy)
  → response flows back the same path
```

### SSH Connection (user → spot's container)

```
User SSH client
  → sentinel:22
  → sshpiper (config: upstream host 127.0.0.2:22)
  → TCP proxy listener on sentinel
  → yamux stream (port header: 22)
  → spot's tunnel client
  → 127.0.0.1:22 on spot (sshd)
```

### Health Check (sentinel monitoring)

```
Sentinel health check loop
  → TCP connect to 127.0.0.2:8080
  → TCP proxy listener on sentinel
  → yamux stream (port header: 8080)
  → spot's tunnel client
  → 127.0.0.1:8080 on spot (containarium daemon /health)
```

### Key Sync (sshpiper configuration)

```
Sentinel KeyStore sync
  → HTTP GET http://127.0.0.2:8080/authorized-keys
  → TCP proxy listener on sentinel
  → yamux stream (port header: 8080)
  → spot's tunnel client
  → 127.0.0.1:8080 on spot (containarium daemon)
  → JSON response with user keys
```

## Hybrid Mode Failover

In hybrid mode, the sentinel manages multiple backends with automatic failover:

```
GCP healthy + Tunnel healthy  → PROXY via GCP (primary), SSH routes to both
GCP preempted + Tunnel healthy → PROXY via Tunnel (failover), sentinel restarts GCP VM
GCP healthy + Tunnel disconnects → PROXY via GCP (no interruption)
Both down                      → MAINTENANCE page served
GCP recovers                   → PROXY via GCP (failback to primary)
```

**HTTP/HTTPS routing**: always goes to the highest-priority healthy backend (GCP > Tunnel).

**SSH routing**: per-user — each user is routed to the backend they were synced from. This means GCP users keep SSHing to GCP, and bare metal users SSH to the tunnel backend, regardless of which backend is primary for HTTP.

## State Transitions

```
                    ┌──── Spot connects via tunnel
                    │     (OnTunnelConnect callback)
                    ▼
              ┌───────────┐
              │ PROXY     │ ← health checks pass (127.0.0.2:8080 reachable via tunnel)
              │ mode      │
              └─────┬─────┘
                    │
                    │ Tunnel disconnects OR health checks fail
                    │ (OnTunnelDisconnect callback)
                    ▼
              ┌───────────┐
              │MAINTENANCE│ → serves 503 page, waits for spot to reconnect
              │ mode      │
              └───────────┘
```

Unlike the standard sentinel (which can restart a stopped GCP VM), the tunnel provider **cannot remotely start** a firewalled spot. The spot must reconnect on its own.

## Requirements

### Sentinel

- Linux (for iptables DNAT and loopback aliases)
- Port 443 open for inbound (already required for HTTPS)
- `net.ipv4.conf.all.route_localnet=1` sysctl (set automatically when DNAT targets loopback)

### Firewalled Spot VM

- **Outbound TCP to port 443** on the sentinel's public IP (most firewalls allow this)
- No inbound ports needed
- Running containarium daemon and services locally (sshd, Caddy, etc.)
- The `containarium` binary installed

## Comparison with Standard Sentinel

| Aspect | Standard (same VPC) | Tunnel (firewalled) |
|--------|--------------------|--------------------|
| Spot → Sentinel connectivity | VPC internal IP | Outbound TCP:443 |
| Extra ports | None | None (shares 443) |
| Latency | Sub-millisecond (VPC) | ~1-10ms (internet + yamux) |
| Auto-recovery | Sentinel restarts VM via GCP API | Spot must reconnect itself |
| Health check | TCP to VPC private IP | TCP to loopback alias → yamux → spot |
| Event watcher | GCP Operations API (preemption detection) | N/A (tunnel disconnect = immediate maintenance) |
| Max spots | Limited by sentinel resources | Same (one loopback alias per spot) |

## Testing

### Run all tunnel tests locally

```bash
go test ./internal/sentinel/ -run "TestTunnel|TestConnMux" -v
```

### Integration test

The `TestTunnelIntegration` test verifies the full flow on localhost:
- Mock spot daemon (health + authorized-keys endpoints)
- ConnMux protocol detection
- Tunnel handshake and yamux session
- HTTP requests flowing through yamux streams

```bash
go test ./internal/sentinel/ -run TestTunnelIntegration -v
```

### Manual local testing

```bash
# Terminal 1: Start a mock spot daemon
python3 -m http.server 8080 &

# Terminal 2: Start sentinel in tunnel mode
containarium sentinel --provider=tunnel --tunnel-token test123 \
  --http-port 9080 --https-port 9443 --forwarded-ports 80,443

# Terminal 3: Connect spot via tunnel
containarium tunnel --sentinel-addr 127.0.0.1:9443 \
  --token test123 --spot-id test-spot --ports 8080
```

### What requires Linux to test

- iptables DNAT to loopback aliases (127.0.0.2)
- sshpiper integration
- Full proxy mode port forwarding

## Security Considerations

- The tunnel token should be a strong random secret (e.g., `openssl rand -hex 32`)
- The tunnel rides on port 443 alongside HTTPS — anyone can attempt a handshake
- Invalid tokens are rejected before yamux session creation
- Future: upgrade to mutual TLS for stronger authentication
- The yamux session carries all traffic unencrypted between sentinel and spot — use this over a trusted network or wrap with TLS

## Source Code

| File | Description |
|------|-------------|
| `internal/sentinel/tunnel_mux.go` | ConnMux: first-byte protocol detection, chanListener, HTTPSProxy |
| `internal/sentinel/tunnel_server.go` | TunnelServer: accepts connections, yamux client, local TCP proxies |
| `internal/sentinel/tunnel_client.go` | TunnelClient: outbound connection, yamux server, port forwarding |
| `internal/sentinel/tunnel_registry.go` | TunnelRegistry: spot tracking, loopback alias management |
| `internal/sentinel/tunnel_provider.go` | TunnelProvider: CloudProvider impl using tunnel state |
| `internal/sentinel/tunnel_auth.go` | Handshake types, JSON encode/decode, token validation |
| `internal/sentinel/tunnel_test.go` | Unit tests: handshake, registry, E2E, wrong token, ConnMux |
| `internal/sentinel/tunnel_integration_test.go` | Full integration test with mock spot services |
| `internal/cmd/tunnel.go` | `containarium tunnel` CLI subcommand |
| `internal/cmd/sentinel.go` | `--provider=tunnel` wiring with ConnMux |

## Related Documents

- [SENTINEL-DESIGN.md](SENTINEL-DESIGN.md) — Standard sentinel architecture
- [SPOT-RECOVERY.md](SPOT-RECOVERY.md) — Recovery timelines
- [SSH-JUMP-SERVER-SETUP.md](SSH-JUMP-SERVER-SETUP.md) — sshpiper configuration
