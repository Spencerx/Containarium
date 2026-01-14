#!/bin/bash
# Containarium Manual Installation Script
#
# This script installs Containarium and all dependencies on a fresh Ubuntu 24.04 system.
# It's an alternative to Terraform for manual or development deployments.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/footprintai/containarium/main/hacks/install.sh | sudo bash
#   or
#   sudo ./hacks/install.sh

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
CONTAINARIUM_VERSION="${CONTAINARIUM_VERSION:-latest}"
INCUS_VERSION_REQUIRED="6.19"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/containarium"
DATA_DIR="/var/lib/containarium"

# Helper functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_root() {
    if [ "$EUID" -ne 0 ]; then
        log_error "This script must be run as root"
        log_info "Please run: sudo $0"
        exit 1
    fi
}

check_os() {
    log_info "Checking operating system..."

    if [ ! -f /etc/os-release ]; then
        log_error "Cannot detect OS. /etc/os-release not found."
        exit 1
    fi

    . /etc/os-release

    if [ "$ID" != "ubuntu" ]; then
        log_warn "This script is designed for Ubuntu. Detected: $ID"
        log_warn "Continuing anyway, but issues may occur..."
    fi

    if [ "$VERSION_ID" != "24.04" ] && [ "$VERSION_ID" != "22.04" ]; then
        log_warn "Recommended version: Ubuntu 24.04. Detected: $VERSION_ID"
        log_warn "Continuing anyway..."
    fi

    log_success "OS check passed: $PRETTY_NAME"
}

install_dependencies() {
    log_info "Installing system dependencies..."

    apt-get update
    apt-get install -y \
        curl \
        wget \
        gnupg \
        ca-certificates \
        software-properties-common \
        jq \
        zfsutils-linux

    log_success "System dependencies installed"
}

install_incus() {
    log_info "Checking Incus installation..."

    # Check if Incus is already installed
    if command -v incus &> /dev/null; then
        INCUS_CURRENT_VERSION=$(incus --version | cut -d'-' -f1)
        log_info "Incus already installed: version $INCUS_CURRENT_VERSION"

        # Compare versions
        if [ "$(printf '%s\n' "$INCUS_VERSION_REQUIRED" "$INCUS_CURRENT_VERSION" | sort -V | head -n1)" == "$INCUS_VERSION_REQUIRED" ]; then
            log_success "Incus version is sufficient (>= $INCUS_VERSION_REQUIRED)"
            return 0
        else
            log_warn "Incus version $INCUS_CURRENT_VERSION is too old. Need >= $INCUS_VERSION_REQUIRED"
            log_info "Upgrading Incus..."
        fi
    fi

    # CRITICAL: Remove ALL Ubuntu Incus packages BEFORE adding Zabbly repository
    # This prevents APT from trying to mix packages from both repositories
    log_info "Removing any existing Ubuntu Incus packages to avoid conflicts..."
    apt-get remove -y incus incus-tools incus-client incus-base incus-ui-canonical 2>/dev/null || true
    apt-get autoremove -y 2>/dev/null || true

    log_info "Installing Incus from Zabbly repository..."

    # Add Zabbly repository (use --batch to avoid TTY issues in non-interactive SSH)
    curl -fsSL https://pkgs.zabbly.com/key.asc | gpg --batch --yes --dearmor -o /usr/share/keyrings/zabbly-incus.gpg

    # Detect Ubuntu codename
    . /etc/os-release
    UBUNTU_CODENAME=$(lsb_release -cs)

    echo "deb [signed-by=/usr/share/keyrings/zabbly-incus.gpg] https://pkgs.zabbly.com/incus/stable ${UBUNTU_CODENAME} main" | \
        tee /etc/apt/sources.list.d/zabbly-incus-stable.list

    # Create APT preference to prioritize Zabbly repository over Ubuntu for Incus packages
    log_info "Configuring APT to prefer Zabbly Incus packages..."
    cat > /etc/apt/preferences.d/zabbly-incus << 'EOF'
Package: incus incus-* *incus*
Pin: origin pkgs.zabbly.com
Pin-Priority: 1000

Package: incus incus-* *incus*
Pin: release o=Ubuntu
Pin-Priority: -1
EOF

    # Update package lists with new repository
    apt-get update

    # Install Incus from Zabbly repository
    # Note: incus-tools was replaced by incus-extra in newer versions
    log_info "Installing incus, incus-client, and incus-extra from Zabbly..."
    apt-get install -y incus incus-client incus-extra

    # Verify installation
    INCUS_VERSION=$(incus --version | cut -d'-' -f1)
    log_success "Incus $INCUS_VERSION installed"

    # Check if initialization is needed
    if ! incus info &> /dev/null; then
        log_info "Initializing Incus with default settings..."
        incus admin init --auto
        log_success "Incus initialized"
    else
        log_info "Incus already initialized"
    fi
}

configure_zfs() {
    log_info "Configuring ZFS..."

    # Check if ZFS module is loaded
    if ! lsmod | grep -q zfs; then
        log_info "Loading ZFS kernel module..."
        modprobe zfs
    fi

    # Ensure ZFS loads on boot
    if ! grep -q "^zfs$" /etc/modules-load.d/zfs.conf 2>/dev/null; then
        echo "zfs" > /etc/modules-load.d/zfs.conf
        log_success "ZFS module configured to load on boot"
    fi

    log_success "ZFS configured"
}

configure_kernel_modules() {
    log_info "Configuring kernel modules for Docker support..."

    # Required modules for Docker in containers
    MODULES=("overlay" "br_netfilter" "nf_nat")

    for module in "${MODULES[@]}"; do
        if ! lsmod | grep -q "^$module"; then
            log_info "Loading kernel module: $module"
            modprobe "$module"
        fi

        if ! grep -q "^$module$" /etc/modules-load.d/containarium.conf 2>/dev/null; then
            echo "$module" >> /etc/modules-load.d/containarium.conf
        fi
    done

    log_success "Kernel modules configured"
}

install_containarium_binary() {
    log_info "Installing Containarium binary..."

    # Determine architecture
    ARCH=$(uname -m)
    case $ARCH in
        x86_64)
            ARCH="amd64"
            ;;
        aarch64)
            ARCH="arm64"
            ;;
        *)
            log_error "Unsupported architecture: $ARCH"
            exit 1
            ;;
    esac

    # Download binary
    if [ "$CONTAINARIUM_VERSION" == "latest" ]; then
        DOWNLOAD_URL="https://github.com/footprintai/containarium/releases/latest/download/containarium-linux-${ARCH}"
    else
        DOWNLOAD_URL="https://github.com/footprintai/containarium/releases/download/${CONTAINARIUM_VERSION}/containarium-linux-${ARCH}"
    fi

    log_info "Downloading from: $DOWNLOAD_URL"

    if ! curl -fsSL "$DOWNLOAD_URL" -o /tmp/containarium; then
        log_error "Failed to download Containarium binary"
        log_info "Please check if the release exists: https://github.com/footprintai/containarium/releases"
        exit 1
    fi

    # Install binary
    install -m 755 /tmp/containarium "$INSTALL_DIR/containarium"
    rm /tmp/containarium

    # Verify installation
    INSTALLED_VERSION=$("$INSTALL_DIR/containarium" version)
    log_success "Containarium installed: $INSTALLED_VERSION"
}

generate_tls_certificates() {
    log_info "Generating TLS certificates for mTLS..."

    # Check if certificates already exist
    if [ -f "$CONFIG_DIR/certs/server.crt" ]; then
        log_info "TLS certificates already exist"
        return 0
    fi

    # Generate certificates
    "$INSTALL_DIR/containarium" cert generate --output "$CONFIG_DIR/certs"

    log_success "TLS certificates generated: $CONFIG_DIR/certs"
}

setup_jwt_secret() {
    log_info "Setting up JWT secret for REST API..."

    # Create config directory
    mkdir -p "$CONFIG_DIR"
    chmod 700 "$CONFIG_DIR"

    # Generate JWT secret if it doesn't exist
    if [ ! -f "$CONFIG_DIR/jwt.secret" ]; then
        openssl rand -base64 32 > "$CONFIG_DIR/jwt.secret"
        chmod 600 "$CONFIG_DIR/jwt.secret"
        log_success "JWT secret generated: $CONFIG_DIR/jwt.secret"
    else
        log_info "JWT secret already exists: $CONFIG_DIR/jwt.secret"
    fi
}

create_systemd_service() {
    log_info "Creating systemd service..."

    cat > /etc/systemd/system/containarium.service << 'EOF'
[Unit]
Description=Containarium Container Management Daemon
Documentation=https://github.com/footprintai/containarium
After=network-online.target incus.service
Wants=network-online.target
Requires=incus.service

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/containarium daemon --mtls
Restart=on-failure
RestartSec=5s
StandardOutput=journal
StandardError=journal

# Security
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=false
ProtectHome=false
ReadWritePaths=/etc/containarium

# Environment
Environment="PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

[Install]
WantedBy=multi-user.target
EOF

    # Reload systemd
    systemctl daemon-reload

    log_success "Systemd service created"
}

setup_firewall() {
    log_info "Configuring firewall..."

    # Check if ufw is installed
    if command -v ufw &> /dev/null; then
        # Allow SSH
        ufw allow 22/tcp comment 'SSH' || true

        # Allow gRPC
        ufw allow 50051/tcp comment 'Containarium gRPC' || true

        # Allow REST API
        ufw allow 8080/tcp comment 'Containarium REST API' || true

        # Enable firewall if not already enabled
        if ! ufw status | grep -q "Status: active"; then
            log_warn "UFW is installed but not active. Enable with: sudo ufw enable"
        else
            log_success "Firewall rules configured"
        fi
    else
        log_warn "UFW not installed. Consider installing for security: apt install ufw"
    fi
}

generate_initial_token() {
    log_info "Generating initial admin token..."

    if [ -f "$CONFIG_DIR/jwt.secret" ]; then
        TOKEN=$("$INSTALL_DIR/containarium" token generate \
            --username admin \
            --roles admin \
            --expiry 720h \
            --secret-file "$CONFIG_DIR/jwt.secret" 2>/dev/null | grep "^eyJ" || echo "")

        if [ -n "$TOKEN" ]; then
            echo "$TOKEN" > "$CONFIG_DIR/admin.token"
            chmod 600 "$CONFIG_DIR/admin.token"
            log_success "Admin token saved to: $CONFIG_DIR/admin.token"
        fi
    fi
}

print_completion_message() {
    echo ""
    echo "═══════════════════════════════════════════════════════════════"
    echo -e "${GREEN}  ✅ Containarium Installation Complete!${NC}"
    echo "═══════════════════════════════════════════════════════════════"
    echo ""
    echo "📦 Installed Components:"
    echo "   • Containarium $(containarium version)"
    echo "   • Incus $(incus --version)"
    echo "   • ZFS kernel module"
    echo ""
    echo "🔧 Configuration:"
    echo "   • Config directory: $CONFIG_DIR"
    echo "   • JWT secret: $CONFIG_DIR/jwt.secret"
    if [ -f "$CONFIG_DIR/admin.token" ]; then
        echo "   • Admin token: $CONFIG_DIR/admin.token"
    fi
    echo "   • Systemd service: /etc/systemd/system/containarium.service"
    echo ""
    echo "🚀 Next Steps:"
    echo ""
    echo "   1. Start the daemon:"
    echo "      sudo systemctl start containarium"
    echo ""
    echo "   2. Enable auto-start on boot:"
    echo "      sudo systemctl enable containarium"
    echo ""
    echo "   3. Check status:"
    echo "      sudo systemctl status containarium"
    echo ""
    echo "   4. View logs:"
    echo "      sudo journalctl -u containarium -f"
    echo ""
    echo "   5. Use the CLI:"
    echo "      sudo containarium list"
    echo "      sudo containarium create alice --ssh-key ~/.ssh/id_rsa.pub"
    echo ""
    echo "   6. Use the REST API:"
    if [ -f "$CONFIG_DIR/admin.token" ]; then
        echo "      export TOKEN=\$(cat $CONFIG_DIR/admin.token)"
        echo "      curl -H \"Authorization: Bearer \$TOKEN\" http://localhost:8080/v1/containers"
    else
        echo "      TOKEN=\$(sudo containarium token generate --username admin --secret-file $CONFIG_DIR/jwt.secret)"
        echo "      curl -H \"Authorization: Bearer \$TOKEN\" http://localhost:8080/v1/containers"
    fi
    echo ""
    echo "   7. Access Swagger UI:"
    echo "      http://$(hostname -I | awk '{print $1}'):8080/swagger-ui/"
    echo ""
    echo "📚 Documentation:"
    echo "   • GitHub: https://github.com/footprintai/containarium"
    echo "   • REST API: https://github.com/footprintai/containarium/blob/main/docs/REST-API-QUICKSTART.md"
    echo ""
    echo "═══════════════════════════════════════════════════════════════"
    echo ""
}

# Main installation flow
main() {
    echo ""
    echo "═══════════════════════════════════════════════════════════════"
    echo "  Containarium Installation Script"
    echo "═══════════════════════════════════════════════════════════════"
    echo ""

    check_root
    check_os
    install_dependencies
    install_incus
    configure_zfs
    configure_kernel_modules
    install_containarium_binary
    generate_tls_certificates
    setup_jwt_secret
    create_systemd_service
    setup_firewall
    generate_initial_token
    print_completion_message
}

# Run main function
main "$@"
