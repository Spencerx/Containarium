#!/bin/bash
#
# Phase A of the lab pool bring-up: install the binary and run the tunnel
# client only. The handshake auto-promotes this node into the sentinel's
# primary registry for pool=lab; verifying that confirms slices 1-7 work
# end-to-end against production.
#
# Phase B (Incus + daemon + core containers) follows once Phase A is green.
#
# Usage (on the lab node, with sudo):
#   sudo bash install-lab-tunnel.sh
#
# Idempotent: re-running replaces the unit and restarts the service.

set -euo pipefail

if [[ $EUID -ne 0 ]]; then
    echo "Error: this script must be run as root (use sudo)"
    exit 1
fi

BINARY_SRC="${BINARY_SRC:-/tmp/containarium}"
BINARY_DST="/usr/local/bin/containarium"

LAB_TOKEN="912029bbfca64e4ec11dcbf8556d8db0b2a15f58c9a7b97bfaa3ccccf0f55186"
SENTINEL_ADDR="containarium.kafeido.app:443"
SPOT_ID="lab-primary-1"
POOL="lab"
PUBLIC_HOSTNAME="containarium-lab.kafeido.app"
PUBLIC_PORT="443"

if [[ ! -f "$BINARY_SRC" ]]; then
    echo "Error: $BINARY_SRC not found. SCP the binary there first:"
    echo "  scp bin/containarium-linux-amd64 ubuntu@<host>:/tmp/containarium"
    exit 1
fi

echo "==> Installing binary -> $BINARY_DST"
install -m 0755 "$BINARY_SRC" "$BINARY_DST"
echo "    md5: $(md5sum "$BINARY_DST" | cut -d' ' -f1)"

echo "==> Writing systemd unit"
cat > /etc/systemd/system/containarium-tunnel.service <<TUNNEL
[Unit]
Description=Containarium Tunnel Client (${POOL} pool primary)
Documentation=https://github.com/footprintai/Containarium
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${BINARY_DST} tunnel \\
  --sentinel-addr ${SENTINEL_ADDR} \\
  --token ${LAB_TOKEN} \\
  --spot-id ${SPOT_ID} \\
  --ports 22,8080,443 \\
  --pool ${POOL} \\
  --public-hostname ${PUBLIC_HOSTNAME} \\
  --public-port ${PUBLIC_PORT}
Restart=always
RestartSec=5s
TimeoutStopSec=10s
User=root
Group=root
StandardOutput=journal
StandardError=journal
SyslogIdentifier=containarium-tunnel
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
TUNNEL

systemctl daemon-reload
systemctl enable --now containarium-tunnel
sleep 3

echo "==> Service status"
systemctl status containarium-tunnel --no-pager | head -12 || true
echo
echo "==> Recent journal"
journalctl -u containarium-tunnel --no-pager -n 10 --since='30 seconds ago' || true
