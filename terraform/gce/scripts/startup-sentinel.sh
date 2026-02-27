#!/bin/bash
set -euo pipefail

# Sentinel VM startup script — minimal setup
# Only installs the containarium binary and runs it in sentinel mode.
# No Incus, no Caddy, no ZFS — just networking and the binary.

exec 1> >(logger -s -t containarium-sentinel-startup) 2>&1
echo "=== Containarium Sentinel Startup ==="

# Template variables
ADMIN_USERS="${join(",", admin_users)}"
CONTAINARIUM_BINARY_URL="${containarium_binary_url}"
SPOT_VM_NAME="${spot_vm_name}"
ZONE="${zone}"
PROJECT_ID="${project_id}"

# System update (minimal)
apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq curl fail2ban > /dev/null

# Harden SSH + add management port 2222 (port 22 gets DNAT'd to spot VM in proxy mode)
cat > /etc/ssh/sshd_config.d/containarium.conf <<EOF
PasswordAuthentication no
PubkeyAuthentication yes
PermitRootLogin no
EOF
# Add port 2222 to main sshd_config (drop-in Port directives may not be additive)
grep -q "^Port 2222" /etc/ssh/sshd_config || echo -e "\nPort 22\nPort 2222" >> /etc/ssh/sshd_config
systemctl restart ssh || systemctl restart sshd || true

# Create admin users
IFS=',' read -ra USERS <<< "$ADMIN_USERS"
for username in "$${USERS[@]}"; do
    username=$(echo "$username" | xargs)
    if [ -z "$username" ]; then continue; fi
    if ! id "$username" &>/dev/null; then
        useradd -m -s /bin/bash -G sudo "$username"
        echo "$username ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/"$username"
        chmod 440 /etc/sudoers.d/"$username"
        mkdir -p /home/"$username"/.ssh
        chmod 700 /home/"$username"/.ssh
        chown -R "$username":"$username" /home/"$username"/.ssh
    fi
done

# Download containarium binary
if [ -n "$CONTAINARIUM_BINARY_URL" ]; then
    echo "Downloading containarium from $CONTAINARIUM_BINARY_URL"
    curl -fsSL "$CONTAINARIUM_BINARY_URL" -o /usr/local/bin/containarium
    chmod +x /usr/local/bin/containarium
    echo "Containarium version: $(/usr/local/bin/containarium version 2>/dev/null || echo 'unknown')"
fi

# Install and start sentinel service
if [ -f /usr/local/bin/containarium ]; then
    /usr/local/bin/containarium sentinel service install \
        --spot-vm "$SPOT_VM_NAME" \
        --zone "$ZONE" \
        --project "$PROJECT_ID"
    echo "Sentinel service installed and running"
else
    echo "WARNING: containarium binary not found, sentinel not started"
fi

echo "=== Sentinel Startup Complete ==="
