# Hacks Directory

Utility scripts for manual installation, development, and testing of Containarium.

## Scripts

### 🚀 `install.sh` - Manual Installation

One-command installation of Containarium and all dependencies on Ubuntu.

**Quick Install:**
```bash
curl -fsSL https://raw.githubusercontent.com/footprintai/containarium/main/hacks/install.sh | sudo bash
```

**Or download and run:**
```bash
wget https://raw.githubusercontent.com/footprintai/containarium/main/hacks/install.sh
chmod +x install.sh
sudo ./install.sh
```

**What it does:**
- ✅ Checks OS and prerequisites
- ✅ Installs Incus 6.19+ from Zabbly repository
- ✅ Configures ZFS storage
- ✅ Loads required kernel modules (overlay, br_netfilter, nf_nat)
- ✅ Downloads and installs Containarium binary
- ✅ Generates JWT secret for REST API
- ✅ Creates systemd service
- ✅ Configures firewall (UFW)
- ✅ Generates initial admin token

**Environment Variables:**
- `CONTAINARIUM_VERSION` - Version to install (default: `latest`)
  ```bash
  CONTAINARIUM_VERSION=v0.3.0 sudo ./hacks/install.sh
  ```

---

### 🗑️ `uninstall.sh` - Complete Removal

Removes Containarium completely (keeps Incus by default).

```bash
sudo ./hacks/uninstall.sh
```

**Options:**
```bash
# Remove Containarium only (keep Incus)
sudo ./hacks/uninstall.sh

# Remove Containarium AND Incus
sudo ./hacks/uninstall.sh --purge-incus

# Remove everything including containers
sudo ./hacks/uninstall.sh --purge-all
```

---

## Use Cases

### Development Testing

```bash
# Install on a test VM
sudo ./hacks/install.sh

# Test your changes
sudo systemctl stop containarium
sudo cp my-new-binary /usr/local/bin/containarium
sudo systemctl start containarium

# Clean up when done
sudo ./hacks/uninstall.sh
```

### Quick Demo Setup

```bash
# Set up demo environment in minutes
curl -fsSL https://raw.githubusercontent.com/footprintai/containarium/main/hacks/install.sh | sudo bash

# Start using immediately
sudo systemctl start containarium
sudo containarium create demo-user
```

### CI/CD Testing

```bash
# In your CI pipeline
- name: Install Containarium
  run: |
    wget https://raw.githubusercontent.com/footprintai/containarium/main/hacks/install.sh
    sudo bash install.sh

- name: Test
  run: |
    sudo systemctl start containarium
    sudo containarium list
```

---

## Supported Systems

- ✅ Ubuntu 24.04 LTS (Noble) - Recommended
- ✅ Ubuntu 22.04 LTS (Jammy)
- ⚠️ Other Debian-based systems - May work but untested

---

## Manual vs Terraform

| Aspect | Manual Install (`hacks/install.sh`) | Terraform |
|--------|-------------------------------------|-----------|
| **Use Case** | Single server, development, testing | Production, multiple servers |
| **Infrastructure** | Existing server | Creates GCE VMs |
| **Time** | ~5 minutes | ~10 minutes |
| **Configuration** | Script defaults | Fully customizable |
| **Idempotent** | Yes (safe to re-run) | Yes |
| **Networking** | Manual setup | Automatic VPC/firewall |
| **High Availability** | Manual | Load balancer + multiple VMs |

**Choose Manual Install when:**
- Testing on a single VM
- Development environment
- Learning/experimenting
- Quick demo setup
- Existing infrastructure

**Choose Terraform when:**
- Production deployment
- Multiple jump servers
- Need load balancing
- Infrastructure as Code
- Reproducible deployments

---

## Troubleshooting

### Installation fails with "Cannot download binary"

The script tries to download from GitHub releases. If you haven't created a release yet:

```bash
# Option 1: Build locally and copy
make build-linux
scp bin/containarium-linux-amd64 server:/tmp/
ssh server "sudo install -m 755 /tmp/containarium-linux-amd64 /usr/local/bin/containarium"

# Then run the rest of the setup
sudo ./hacks/install.sh --skip-binary
```

### Incus package conflict (Ubuntu vs Zabbly)

**Error:**
```
incus-base : Breaks: incus-tools but 6.0.0-1ubuntu0.3 is to be installed
E: Unable to correct problems, you have held broken packages.
```

**Cause:** APT tries to mix packages from Ubuntu's repository (Incus 6.0.0) and Zabbly's repository (Incus 6.19+).

**Solution:**
```bash
# Quick fix script
sudo ./hacks/fix-incus-conflict.sh
```

**What it does:**
1. Removes all Ubuntu Incus packages
2. Adds Zabbly repository
3. Creates APT pinning rules to prefer Zabbly packages
4. Installs Incus from Zabbly

**Note:** The install script (`install.sh`) now includes these fixes automatically, so this should not occur on fresh installations.

### Incus initialization fails

```bash
# Manually initialize Incus
sudo incus admin init --auto

# Then re-run the script (it will skip Incus installation)
sudo ./hacks/install.sh
```

### Systemd service won't start

```bash
# Check logs
sudo journalctl -u containarium -n 50

# Common issues:
# 1. Incus not running
sudo systemctl start incus

# 2. Missing certificates (if using mTLS without REST)
sudo containarium cert generate

# 3. JWT secret missing (if using REST)
openssl rand -base64 32 | sudo tee /etc/containarium/jwt.secret
```

---

## Files Created

The installation script creates:

```
/usr/local/bin/containarium          # Binary
/etc/containarium/
├── jwt.secret                       # JWT secret for REST API
└── admin.token                      # Initial admin token (if generated)
/etc/systemd/system/containarium.service  # Systemd service
/etc/modules-load.d/containarium.conf     # Kernel modules
```

---

## Security Notes

- The script requires root access
- JWT secret is generated with strong randomness
- Firewall rules are configured for SSH, gRPC, and REST API
- Systemd service runs as root (required for container management)
- All config files are chmod 600 (only root can read)

---

## Contributing

To add new scripts to this directory:

1. Make them executable: `chmod +x hacks/your-script.sh`
2. Add shebang: `#!/bin/bash`
3. Include usage documentation in comments
4. Update this README
5. Test on fresh Ubuntu 24.04 VM

---

## Related Documentation

- [Installation Guide](../docs/INSTALLATION.md)
- [REST API Quick Start](../docs/REST-API-QUICKSTART.md)
- [Terraform Deployment](../terraform/gce/README.md)
