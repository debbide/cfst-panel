#!/usr/bin/env bash
set -euo pipefail

# Minimal Ubuntu deploy helper.
# Binary already bundles CloudflareST + web UI.
# Usage:
#   sudo bash scripts/deploy-ubuntu.sh /opt/cfst-panel

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
INSTALL_DIR="${1:-/opt/cfst-panel}"
BIN_SRC="$ROOT_DIR/dist/cfst-panel"

if [[ ! -f "$BIN_SRC" ]]; then
  echo "missing $BIN_SRC"
  echo "build first: CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildvcs=false -o dist/cfst-panel ./cmd/server"
  exit 1
fi

mkdir -p "$INSTALL_DIR"
install -m 0755 "$BIN_SRC" "$INSTALL_DIR/cfst-panel"

if [[ -f "$ROOT_DIR/deploy/cfst-panel.service" ]]; then
  sed "s#/opt/cfst-panel#$INSTALL_DIR#g" "$ROOT_DIR/deploy/cfst-panel.service" > /tmp/cfst-panel.service
  if [[ $EUID -eq 0 ]]; then
    id -u cfst >/dev/null 2>&1 || useradd --system --home "$INSTALL_DIR" --shell /usr/sbin/nologin cfst
    chown -R cfst:cfst "$INSTALL_DIR"
    install -m 0644 /tmp/cfst-panel.service /etc/systemd/system/cfst-panel.service
    systemctl daemon-reload
    systemctl enable --now cfst-panel
    echo "service started: systemctl status cfst-panel"
  else
    echo "binary installed to $INSTALL_DIR"
    echo "run manually:"
    echo "  $INSTALL_DIR/cfst-panel --addr 0.0.0.0:8787"
  fi
fi
