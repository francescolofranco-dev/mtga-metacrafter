#!/usr/bin/env bash
# install.sh — one-shot setup for a fresh Ubuntu VM (Oracle Cloud Always Free
# Ampere A1 in mind, but works on any modern Ubuntu).
#
# Run as root from the VM:  sudo bash /tmp/install.sh
#
# Does NOT install or start the application binary — that's left to the
# deploy step (manual scp or the GitHub Actions workflow). This script
# only sets up the durable plumbing:
#   * opens 80/443 in iptables (Oracle ships locked-down by default)
#   * creates a 'metacrafter' system user with no login shell
#   * lays out /opt/mtga-metacrafter/{bin,data}
#   * installs Caddy and writes a default Caddyfile
#   * writes and enables the mtga-metacrafter systemd unit
set -euo pipefail

APP_NAME="mtga-metacrafter"
APP_USER="metacrafter"
APP_HOME="/opt/${APP_NAME}"
APP_BIN="${APP_HOME}/${APP_NAME}"
APP_DATA="${APP_HOME}/data"

# ---------------------------------------------------------------------------
# Sanity
# ---------------------------------------------------------------------------
if [[ $EUID -ne 0 ]]; then
  echo "This script must be run as root (use sudo)." >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Firewall (Oracle's iptables blocks 80/443 by default)
# ---------------------------------------------------------------------------
echo "==> Opening 80/443 in iptables..."
iptables -I INPUT -p tcp --dport 80 -j ACCEPT
iptables -I INPUT -p tcp --dport 443 -j ACCEPT
# Persist across reboots.
apt-get update -y
DEBIAN_FRONTEND=noninteractive apt-get install -y iptables-persistent
netfilter-persistent save

# ---------------------------------------------------------------------------
# App user + layout
# ---------------------------------------------------------------------------
echo "==> Creating ${APP_USER} user + ${APP_HOME} layout..."
if ! id "${APP_USER}" &>/dev/null; then
  useradd --system --no-create-home --shell /usr/sbin/nologin "${APP_USER}"
fi
install -d -o "${APP_USER}" -g "${APP_USER}" -m 0755 "${APP_HOME}" "${APP_DATA}"

# ---------------------------------------------------------------------------
# Caddy
# ---------------------------------------------------------------------------
echo "==> Installing Caddy..."
if ! command -v caddy >/dev/null 2>&1; then
  DEBIAN_FRONTEND=noninteractive apt-get install -y debian-keyring debian-archive-keyring apt-transport-https curl
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
    | tee /etc/apt/sources.list.d/caddy-stable.list >/dev/null
  apt-get update -y
  DEBIAN_FRONTEND=noninteractive apt-get install -y caddy
fi

cat > /etc/caddy/Caddyfile <<'EOF'
# Replace the :443 line with your domain once DNS is pointed here, e.g.
#   metacrafter.example.com {
#       reverse_proxy 127.0.0.1:8080
#   }
# Until then, Caddy serves over plain HTTP on :80.

:80 {
    encode gzip
    reverse_proxy 127.0.0.1:8080
}
EOF
systemctl enable --now caddy
systemctl reload caddy || true

# ---------------------------------------------------------------------------
# systemd unit
# ---------------------------------------------------------------------------
echo "==> Writing systemd unit..."
cat > /etc/systemd/system/${APP_NAME}.service <<EOF
[Unit]
Description=MTGA MetaCrafter
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${APP_USER}
Group=${APP_USER}
WorkingDirectory=${APP_HOME}
ExecStart=${APP_BIN}
Environment=LISTEN_ADDR=127.0.0.1:8080
Environment=DATA_DIR=${APP_DATA}
Environment=REFRESH_PERIOD=24h
Environment=FORMATS=standard,pioneer
Environment=LOG_LEVEL=info
Restart=on-failure
RestartSec=5
NoNewPrivileges=yes
ProtectSystem=full
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=${APP_DATA}
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable ${APP_NAME}.service

echo
echo "==> Done. Next steps:"
echo "  1. Deploy the binary into ${APP_BIN} (manual scp or GH Actions)."
echo "  2. systemctl start ${APP_NAME}"
echo "  3. curl http://localhost/healthz   (proxied through Caddy)"
echo
echo "Edit /etc/caddy/Caddyfile + reload caddy when you point a domain here."
