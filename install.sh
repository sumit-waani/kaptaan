#!/usr/bin/env bash
set -euo pipefail

# ── Configuration ────────────────────────────────────────────────────────────
REPO_URL="${REPO_URL:-https://github.com/sumit-waani/kaptaan}"
INSTALL_DIR="/opt/kaptaan"
DATA_DIR="/var/lib/kaptaan"
BINARY="/usr/local/bin/kaptaan"
SERVICE="kaptaan"
PORT="80"
GO_VERSION="1.24.3"

# ── Colours ──────────────────────────────────────────────────────────────────
GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info()    { echo -e "${GREEN}[kaptaan]${NC} $*"; }
warn()    { echo -e "${YELLOW}[kaptaan]${NC} $*"; }
die()     { echo -e "${RED}[kaptaan] ERROR:${NC} $*" >&2; exit 1; }

# ── Must run as root ─────────────────────────────────────────────────────────
[[ $EUID -eq 0 ]] || die "Run this script with sudo or as root."

# ── Require GitHub PAT (private repo) ────────────────────────────────────────
[[ -n "${GITHUB_TOKEN:-}" ]] || die "GITHUB_TOKEN is required for private repo access.
  Run: sudo GITHUB_TOKEN=ghp_xxx bash install.sh"

# Inject token into the clone URL: https://TOKEN@github.com/owner/repo
# Works whether REPO_URL starts with https:// or not.
AUTH_REPO_URL="${REPO_URL/https:\/\//https:\/\/${GITHUB_TOKEN}@}"

# ── Detect OS / package manager ──────────────────────────────────────────────
if command -v apt-get &>/dev/null; then
    PKG_MGR="apt"
elif command -v yum &>/dev/null; then
    PKG_MGR="yum"
elif command -v dnf &>/dev/null; then
    PKG_MGR="dnf"
else
    die "Unsupported OS — could not find apt-get, yum, or dnf."
fi

# ── Install system dependencies ───────────────────────────────────────────────
info "Installing system dependencies..."
case "$PKG_MGR" in
    apt)
        apt-get update -qq
        apt-get install -y -qq git curl wget tar ca-certificates
        ;;
    yum|dnf)
        $PKG_MGR install -y git curl wget tar ca-certificates
        ;;
esac

# ── Install Go if needed ──────────────────────────────────────────────────────
GOROOT="/usr/local/go"

need_go=false
if ! command -v go &>/dev/null && [[ ! -x "$GOROOT/bin/go" ]]; then
    need_go=true
else
    current_go=$(${GOROOT}/bin/go version 2>/dev/null | awk '{print $3}' | sed 's/go//' || go version | awk '{print $3}' | sed 's/go//')
    if [[ "$(printf '%s\n' "$GO_VERSION" "$current_go" | sort -V | head -1)" != "$GO_VERSION" ]]; then
        warn "Go $current_go is older than required $GO_VERSION — reinstalling."
        need_go=true
    fi
fi

if $need_go; then
    info "Installing Go $GO_VERSION..."
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64)  GO_ARCH="amd64" ;;
        aarch64) GO_ARCH="arm64" ;;
        *)        die "Unsupported architecture: $ARCH" ;;
    esac
    GO_TAR="go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
    wget -q "https://dl.google.com/go/$GO_TAR" -O "/tmp/$GO_TAR"
    rm -rf "$GOROOT"
    tar -C /usr/local -xzf "/tmp/$GO_TAR"
    rm "/tmp/$GO_TAR"
    info "Go $GO_VERSION installed."
fi

export PATH="$GOROOT/bin:$PATH"
go version

# ── Clone or update source ────────────────────────────────────────────────────
if [[ -d "$INSTALL_DIR/.git" ]]; then
    info "Updating existing source in $INSTALL_DIR..."
    # Update the remote URL with the current token, then pull latest.
    git -C "$INSTALL_DIR" remote set-url origin "$AUTH_REPO_URL"
    git -C "$INSTALL_DIR" fetch --quiet origin
    git -C "$INSTALL_DIR" reset --hard origin/HEAD --quiet
    # Remove the token from the stored remote URL so it isn't sitting on disk.
    git -C "$INSTALL_DIR" remote set-url origin "$REPO_URL"
else
    info "Cloning repository into $INSTALL_DIR..."
    git clone --quiet "$AUTH_REPO_URL" "$INSTALL_DIR"
    # Remove the token from the stored remote URL immediately after clone.
    git -C "$INSTALL_DIR" remote set-url origin "$REPO_URL"
fi

# ── Build binary ──────────────────────────────────────────────────────────────
info "Building kaptaan binary..."
cd "$INSTALL_DIR"
go build -o "$BINARY" .
chmod +x "$BINARY"
info "Binary installed at $BINARY"

# ── Create data directory (never delete existing database) ────────────────────
info "Ensuring data directory $DATA_DIR exists..."
mkdir -p "$DATA_DIR"
if [[ -f "$DATA_DIR/kaptaan.db" ]]; then
    info "Existing database found — preserving it."
else
    info "No existing database — it will be created on first start."
fi

# ── Create systemd service ────────────────────────────────────────────────────
info "Writing systemd service..."
cat > "/etc/systemd/system/${SERVICE}.service" <<EOF
[Unit]
Description=Kaptaan autonomous coding agent
After=network.target

[Service]
Type=simple
ExecStart=$BINARY
WorkingDirectory=$DATA_DIR
Environment=DB_PATH=$DATA_DIR/kaptaan.db
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=kaptaan
# Allow binding privileged ports (80) without running as root
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
EOF

# ── Enable and (re)start service ──────────────────────────────────────────────
info "Enabling and starting kaptaan service..."
systemctl daemon-reload
systemctl enable "$SERVICE"

if systemctl is-active --quiet "$SERVICE"; then
    info "Service is running — restarting to pick up new binary..."
    systemctl restart "$SERVICE"
else
    systemctl start "$SERVICE"
fi

# ── Done ──────────────────────────────────────────────────────────────────────
sleep 2
if systemctl is-active --quiet "$SERVICE"; then
    info "kaptaan is running on port 80 and 5000"
    echo ""
    PUBLIC_IP=$(curl -s ifconfig.me 2>/dev/null || echo '<your-lightsail-ip>')
    echo -e "  ${GREEN}Open http://${PUBLIC_IP} in your browser (port 80)${NC}"
    echo -e "  ${GREEN}Or http://${PUBLIC_IP}:5000 (fallback port 5000)${NC}"
    echo -e "  ${YELLOW}Remember to open ports 80 and 5000 in your Lightsail firewall if you haven't already.${NC}"
else
    warn "Service may not have started. Check logs with:"
    echo "  journalctl -u kaptaan -n 50 --no-pager"
fi
