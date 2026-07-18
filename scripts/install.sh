#!/usr/bin/env bash
set -euo pipefail

# CFST Panel one-click installer
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/debbide/cfst-panel/main/scripts/install.sh | bash
#   curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/debbide/cfst-panel/main/scripts/install.sh | bash
#
# Optional env:
#   INSTALL_DIR=/opt/cfst-panel
#   LISTEN_ADDR=0.0.0.0:8787
#   REPO=debbide/cfst-panel
#   VERSION=latest          # or v0.3.3
#   NO_SERVICE=1            # skip systemd
#   FORCE=1                 # overwrite existing binary
#   MIRROR=auto|off|ghfast|ghproxy|moeyy

REPO="${REPO:-debbide/cfst-panel}"
INSTALL_DIR="${INSTALL_DIR:-/opt/cfst-panel}"
LISTEN_ADDR="${LISTEN_ADDR:-0.0.0.0:8787}"
VERSION="${VERSION:-latest}"
NO_SERVICE="${NO_SERVICE:-0}"
FORCE="${FORCE:-0}"
MIRROR="${MIRROR:-auto}"
SERVICE_NAME="cfst-panel"
BIN_NAME="cfst-panel"
SERVICE_USER="cfst"

RED=$'\033[31m'
GREEN=$'\033[32m'
YELLOW=$'\033[33m'
CYAN=$'\033[36m'
RESET=$'\033[0m'

log()  { printf '%s[INFO]%s %s\n'  "$CYAN" "$RESET" "$*"; }
ok()   { printf '%s[OK]%s %s\n'    "$GREEN" "$RESET" "$*"; }
warn() { printf '%s[WARN]%s %s\n'  "$YELLOW" "$RESET" "$*"; }
err()  { printf '%s[ERR]%s %s\n'   "$RED" "$RESET" "$*" >&2; }

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    err "missing command: $1"
    exit 1
  }
}

detect_arch() {
  local m
  m="$(uname -m)"
  case "$m" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *)
      err "unsupported arch: $m (only amd64/arm64)"
      exit 1
      ;;
  esac
}

detect_os() {
  local s
  s="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$s" in
    linux) echo "linux" ;;
    *)
      err "unsupported os: $s (linux only)"
      exit 1
      ;;
  esac
}

have_root() {
  [[ "${EUID:-$(id -u)}" -eq 0 ]]
}

ensure_root() {
  if ! have_root; then
    err "please run as root: sudo bash install.sh"
    exit 1
  fi
}

download() {
  # download <url> <out>
  local url="$1"
  local out="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fL --connect-timeout 15 --retry 3 --retry-delay 1 -o "$out" "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -O "$out" --timeout=20 --tries=3 "$url"
  else
    err "need curl or wget"
    exit 1
  fi
}

http_get() {
  local url="$1"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --connect-timeout 15 --retry 3 --retry-delay 1 "$url"
  else
    wget -qO- --timeout=20 --tries=3 "$url"
  fi
}

mirror_url() {
  # mirror_url <raw-url> <provider>
  local raw="$1"
  local provider="$2"
  case "$provider" in
    direct) echo "$raw" ;;
    ghfast) echo "https://ghfast.top/${raw}" ;;
    ghproxy) echo "https://ghproxy.net/${raw}" ;;
    moeyy) echo "https://github.moeyy.xyz/${raw}" ;;
    *) echo "$raw" ;;
  esac
}

api_url() {
  local path="$1"
  local raw="https://api.github.com${path}"
  case "$MIRROR" in
    off|direct) echo "$raw" ;;
    ghfast) echo "https://ghfast.top/${raw}" ;;
    ghproxy) echo "https://ghproxy.net/${raw}" ;;
    moeyy) echo "https://github.moeyy.xyz/${raw}" ;;
    auto)
      # API usually works direct; keep direct first for JSON stability.
      echo "$raw"
      ;;
    *) echo "$raw" ;;
  esac
}

asset_candidates() {
  # print candidate download URLs for a release asset
  local tag="$1"
  local asset="$2"
  local raw="https://github.com/${REPO}/releases/download/${tag}/${asset}"
  case "$MIRROR" in
    off|direct)
      echo "$raw"
      ;;
    ghfast)
      mirror_url "$raw" ghfast
      echo "$raw"
      ;;
    ghproxy)
      mirror_url "$raw" ghproxy
      echo "$raw"
      ;;
    moeyy)
      mirror_url "$raw" moeyy
      echo "$raw"
      ;;
    auto|*)
      mirror_url "$raw" ghfast
      mirror_url "$raw" ghproxy
      mirror_url "$raw" moeyy
      echo "$raw"
      ;;
  esac
}

fetch_latest_tag() {
  local json tag
  if ! json="$(http_get "$(api_url "/repos/${REPO}/releases/latest")" 2>/dev/null)"; then
    # try accelerated API mirrors if auto mode
    if [[ "$MIRROR" == "auto" ]]; then
      for p in ghfast ghproxy moeyy; do
        if json="$(http_get "$(mirror_url "https://api.github.com/repos/${REPO}/releases/latest" "$p")" 2>/dev/null)"; then
          break
        fi
      done
    fi
  fi
  if [[ -z "${json:-}" ]]; then
    err "failed to query latest release for ${REPO}"
    exit 1
  fi
  tag="$(printf '%s' "$json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
  if [[ -z "$tag" ]]; then
    err "cannot parse latest release tag"
    exit 1
  fi
  printf '%s\n' "$tag"
}

download_asset() {
  local tag="$1"
  local asset="$2"
  local out="$3"
  local url
  local ok_dl=0
  while IFS= read -r url; do
    [[ -z "$url" ]] && continue
    log "try download: $url"
    if download "$url" "$out"; then
      if [[ -s "$out" ]]; then
        ok_dl=1
        ok "downloaded via: $url"
        break
      fi
    fi
    warn "download failed: $url"
    rm -f "$out"
  done < <(asset_candidates "$tag" "$asset")
  if [[ "$ok_dl" -ne 1 ]]; then
    err "all download mirrors failed for $asset"
    exit 1
  fi
}

install_binary() {
  local src="$1"
  mkdir -p "$INSTALL_DIR"
  if [[ -f "$INSTALL_DIR/$BIN_NAME" && "$FORCE" != "1" ]]; then
    if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
      log "stop service $SERVICE_NAME"
      systemctl stop "$SERVICE_NAME" || true
    fi
  fi
  install -m 0755 "$src" "$INSTALL_DIR/$BIN_NAME"
  ok "installed binary: $INSTALL_DIR/$BIN_NAME"
}

ensure_user() {
  if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
    useradd --system --home "$INSTALL_DIR" --shell /usr/sbin/nologin "$SERVICE_USER"
    ok "created user: $SERVICE_USER"
  fi
  chown -R "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR"
}

install_service() {
  local unit="/etc/systemd/system/${SERVICE_NAME}.service"
  cat >"$unit" <<EOF
[Unit]
Description=CFST Panel - Cloudflare preferred IP tester and DNS updater
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_USER}
WorkingDirectory=${INSTALL_DIR}
ExecStart=${INSTALL_DIR}/${BIN_NAME} --addr ${LISTEN_ADDR}
Restart=on-failure
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable --now "$SERVICE_NAME"
  ok "systemd service enabled: $SERVICE_NAME"
}

print_summary() {
  local ip
  ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
  [[ -z "$ip" ]] && ip="服务器IP"
  cat <<EOF

${GREEN}CFST Panel installed${RESET}
  version : ${TAG}
  arch    : ${ARCH}
  path    : ${INSTALL_DIR}/${BIN_NAME}
  listen  : ${LISTEN_ADDR}
  panel   : http://${ip}:${LISTEN_ADDR##*:}
  default : admin / admin123

常用命令:
  systemctl status ${SERVICE_NAME}
  systemctl restart ${SERVICE_NAME}
  journalctl -u ${SERVICE_NAME} -f

重新安装:
  curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/${REPO}/main/scripts/install.sh | sudo bash

EOF
}

main() {
  detect_os >/dev/null
  ARCH="$(detect_arch)"
  ensure_root
  need_cmd uname
  need_cmd install
  need_cmd sed
  if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
    err "need curl or wget"
    exit 1
  fi

  if [[ "$VERSION" == "latest" || -z "$VERSION" ]]; then
    log "query latest release..."
    TAG="$(fetch_latest_tag)"
  else
    TAG="$VERSION"
  fi
  ok "release: $TAG"

  if [[ "$ARCH" == "amd64" ]]; then
    ASSET="cfst-panel"
  else
    ASSET="cfst-panel-arm64"
  fi
  log "asset: $ASSET ($ARCH)"

  TMPDIR="$(mktemp -d)"
  trap 'rm -rf "$TMPDIR"' EXIT
  OUT="$TMPDIR/$ASSET"

  download_asset "$TAG" "$ASSET" "$OUT"
  chmod +x "$OUT"

  install_binary "$OUT"

  if [[ "$NO_SERVICE" == "1" ]]; then
    warn "NO_SERVICE=1, skip systemd"
    cat <<EOF

手动启动:
  ${INSTALL_DIR}/${BIN_NAME} --addr ${LISTEN_ADDR}

EOF
    return 0
  fi

  if ! command -v systemctl >/dev/null 2>&1; then
    warn "systemctl not found, skip service install"
    cat <<EOF

手动启动:
  ${INSTALL_DIR}/${BIN_NAME} --addr ${LISTEN_ADDR}

EOF
    return 0
  fi

  ensure_user
  install_service
  print_summary
}

main "$@"