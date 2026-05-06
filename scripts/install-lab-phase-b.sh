#!/bin/bash
#
# Phase B of the lab pool bring-up: install Incus, initialize it, configure
# the containarium daemon to run with --pool=lab, and let the daemon bring
# up its core containers (postgres, victoriametrics, caddy). After this
# completes successfully, https://containarium-lab.kafeido.app/ should
# eventually serve the WebUI (Caddy fetches its Let's Encrypt cert via
# TLS-ALPN-01 over the SNI route established in slice 8).
#
# Idempotent: re-running skips steps already done. Safe to interrupt and
# resume; the daemon's own state survives across runs.
#
# Usage (on the lab node, with sudo):
#   sudo bash install-lab-phase-b.sh
#
# What it does NOT touch:
#   - The existing containarium-tunnel.service (Phase A — leave running)
#   - The /usr/local/bin/containarium binary (Phase A installed it)

set -euo pipefail

if [[ $EUID -ne 0 ]]; then
    echo "Error: this script must be run as root (use sudo)"
    exit 1
fi

NETWORK_SUBNET="${NETWORK_SUBNET:-10.0.4.1/24}"
POOL="${POOL:-lab}"
BASE_DOMAIN="${BASE_DOMAIN:-containarium-lab.kafeido.app}"

echo "==> Step 1/6: Install Incus from Zabbly"
if command -v incus >/dev/null 2>&1; then
    echo "    Already installed: $(incus version | head -1)"
else
    install -d -m 0755 /etc/apt/keyrings
    curl -fsSL https://pkgs.zabbly.com/key.asc | gpg --dearmor -o /etc/apt/keyrings/zabbly.gpg
    cat > /etc/apt/sources.list.d/zabbly-incus-stable.sources <<EOF
Enabled: yes
Types: deb
URIs: https://pkgs.zabbly.com/incus/stable
Suites: $(lsb_release -cs)
Components: main
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/zabbly.gpg
EOF
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y incus
    echo "    Installed: $(incus version | head -1)"
fi

echo
echo "==> Step 2/6: Initialize Incus (default storage + network)"
if incus storage list -f csv 2>/dev/null | grep -q '^default,'; then
    echo "    Already initialized — storage 'default' exists"
else
    # --auto picks dir backend when ZFS/btrfs isn't available, which
    # matches what we want on this VirtualBox guest. The default network
    # name is incusbr0, which the daemon expects.
    incus admin init --auto --network-address=127.0.0.1 --network-port=8443 --storage-backend=dir
    echo "    Storage + network initialized"
fi

echo
echo "==> Step 3/6: JWT secret for the daemon's REST API"
if [[ ! -s /etc/containarium/jwt.secret ]]; then
    install -d -m 0700 /etc/containarium
    openssl rand -hex 32 > /etc/containarium/jwt.secret
    chmod 600 /etc/containarium/jwt.secret
    echo "    Generated: /etc/containarium/jwt.secret"
else
    echo "    Already exists: /etc/containarium/jwt.secret"
fi

echo
echo "==> Step 4/6: Install daemon systemd unit (containarium.service)"
if ! systemctl cat containarium.service >/dev/null 2>&1; then
    /usr/local/bin/containarium service install
fi
# The unit's ReadWritePaths= includes /opt/containarium. systemd refuses
# to start a service with a missing ReadWritePaths entry ("Failed to set
# up mount namespacing: /opt/containarium: No such file or directory"),
# so create it up-front. The daemon writes its persistent state here.
install -d -m 0755 /opt/containarium

echo
echo "==> Step 5/6: Drop-in override for --pool=${POOL} + --network-subnet=${NETWORK_SUBNET}"
# Notes on flags:
#   - --pool ${POOL}              scopes peer discovery (slice 2)
#   - --rest                      enable HTTP/REST API on :8080
#   - --jwt-secret-file           auth for /v1/* endpoints
#   - --network-subnet            local container bridge subnet
#   - --app-hosting               REQUIRED to spawn core Caddy + VictoriaMetrics
#                                 containers. Without it the daemon auto-detects
#                                 existing core containers but never creates
#                                 them, so a fresh box ends up without Caddy
#                                 and TLS routing has nothing to terminate at.
#   - --base-domain               hostname Caddy auto-configures HTTPS for
#                                 (Let's Encrypt cert via TLS-ALPN-01).
#
# We DO NOT set --sentinel-url here. The lab node is on Tailscale; the
# sentinel's binary server (8888) isn't reachable from there. Peer
# discovery is therefore disabled — fine for a single-node lab pool.
# Primary registration is handled separately via the tunnel handshake
# (slice 6), which is already running in containarium-tunnel.service.
install -d -m 0755 /etc/systemd/system/containarium.service.d
cat > /etc/systemd/system/containarium.service.d/lab-pool.conf <<CONF
[Service]
ExecStart=
ExecStart=/usr/local/bin/containarium daemon \\
  --pool ${POOL} \\
  --network-subnet ${NETWORK_SUBNET} \\
  --rest \\
  --jwt-secret-file /etc/containarium/jwt.secret \\
  --app-hosting \\
  --base-domain ${BASE_DOMAIN}
Environment="CONTAINARIUM_ALLOWED_ORIGINS=https://containarium.kafeido.app,https://${BASE_DOMAIN},http://localhost:3000,http://localhost:8080"
Restart=on-failure
RestartSec=10s
CONF
echo "    Override written: /etc/systemd/system/containarium.service.d/lab-pool.conf"

echo
echo "==> Step 6/6: (Re)start daemon"
systemctl daemon-reload
systemctl enable containarium >/dev/null
# Use restart (not just `start`) so re-running this script picks up any
# changes to the lab-pool.conf override above. systemctl restart is a
# no-op if the unit is already in the desired (running with current
# unit) state, otherwise it does the right thing for both fresh and
# already-running daemons.
systemctl restart containarium

echo
echo "==> Phase B install complete."
echo
echo "The daemon is now starting up. On first boot it pulls down core"
echo "container images (postgres, victoriametrics, caddy) and brings"
echo "them up — typically 3–10 minutes depending on bandwidth."
echo
echo "Watch progress:"
echo "    sudo journalctl -u containarium -f"
echo
echo "Or check container state:"
echo "    sudo incus list"
echo
echo "Once 'containarium-core-caddy' is RUNNING, the lab pool should"
echo "start serving https://containarium-lab.kafeido.app/ (Caddy will"
echo "fetch a Let's Encrypt cert on first request)."
