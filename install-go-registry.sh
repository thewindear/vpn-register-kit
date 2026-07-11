#!/usr/bin/env bash
set -Eeuo pipefail

DRY_RUN=0
if [[ "${1:-}" == "--dry-run" ]]; then
  DRY_RUN=1
fi

APP_USER="vpn-registry"
APP_DIR="/var/lib/vpn-register-kit"
BIN_PATH="/usr/local/bin/vpn-registry"
SERVICE_PATH="/etc/systemd/system/vpn-registry.service"
CONFIG_PATH="$APP_DIR/config.json"

log() {
  printf '[INFO] %s\n' "$*"
}

fail() {
  printf '[ERROR] %s\n' "$*" >&2
  exit 1
}

run_cmd() {
  if [[ "$DRY_RUN" == "1" ]]; then
    printf '[DRY-RUN] %q' "$1"
    shift || true
    for arg in "$@"; do
      printf ' %q' "$arg"
    done
    printf '\n'
    return 0
  fi
  "$@"
}

write_file() {
  local path="$1"
  local mode="$2"
  local tmp
  tmp="$(mktemp)"
  cat >"$tmp"
  if [[ "$DRY_RUN" == "1" ]]; then
    log "would write $path"
    sed 's/^/[DRY-RUN]   /' "$tmp"
    rm -f "$tmp"
    return 0
  fi
  install -D -m "$mode" "$tmp" "$path"
  rm -f "$tmp"
}

ensure_linux_root() {
  [[ "$(uname -s)" == "Linux" ]] || fail "This installer is intended for Linux servers only."
  [[ "$(id -u)" == "0" ]] || fail "Please run as root."
}

main() {
  ensure_linux_root
  command -v go >/dev/null 2>&1 || fail "Go is required to build vpn-registry."
  [[ -f go-registry/main.go ]] || fail "Run this script from the vpn-register-kit project root."

  run_cmd go build -C go-registry -o ../vpn-registry .
  if ! id -u "$APP_USER" >/dev/null 2>&1; then
    run_cmd useradd --system --home "$APP_DIR" --shell /usr/sbin/nologin "$APP_USER"
  fi
  run_cmd mkdir -p "$APP_DIR"
  run_cmd install -m 0755 vpn-registry "$BIN_PATH"
  run_cmd chown -R "$APP_USER:$APP_USER" "$APP_DIR"
  run_cmd chmod 0700 "$APP_DIR"

  write_file "$SERVICE_PATH" "0644" <<EOF
[Unit]
Description=VPN Register Kit registry service
After=network-online.target
Wants=network-online.target

[Service]
User=$APP_USER
Group=$APP_USER
WorkingDirectory=$APP_DIR
ExecStart=$BIN_PATH server -config $CONFIG_PATH
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF

  run_cmd systemctl daemon-reload
  run_cmd systemctl enable vpn-registry
  run_cmd systemctl restart vpn-registry
  log "installed. Check logs with: journalctl -u vpn-registry -f"
}

main "$@"
