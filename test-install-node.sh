#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$(<"$ROOT_DIR/install-node.sh")"

require_contains() {
  local needle="$1"
  if ! grep -Fq "$needle" <<<"$SCRIPT"; then
    printf 'Expected install-node.sh to contain: %s
' "$needle" >&2
    exit 1
  fi
}

require_contains "/etc/letsencrypt/renewal-hooks/deploy/reload-vpn-services"
require_contains "systemctl enable certbot.timer"
require_contains "systemctl restart sing-box"
require_contains "systemctl reload nginx"
require_contains "/etc/sysctl.d/99-vpn-node-network-tuning.conf"
require_contains "net.core.somaxconn = 65535"
require_contains "net.ipv4.tcp_max_syn_backlog = 65535"
require_contains "net.ipv4.ip_local_port_range = 10000 65000"
require_contains "net.ipv4.tcp_fin_timeout = 15"
require_contains "net.ipv4.tcp_max_tw_buckets = 2000000"
require_contains "net.ipv4.tcp_fastopen = 3"
require_contains "/etc/systemd/system/nginx.service.d/limits.conf"
require_contains "LimitNOFILE=524288"
require_contains "TasksMax=infinity"
require_contains "worker_rlimit_nofile 200000;"
require_contains "worker_connections 8192;"
require_contains "multi_accept on;"
require_contains "use epoll;"
require_contains "keepalive_timeout 15;"
require_contains "keepalive_requests 1000;"
require_contains "listen 80 backlog=65535;"
require_contains "listen [::]:80 backlog=65535;"
require_contains 'listen 127.0.0.1:$FALLBACK_PORT backlog=65535;'
require_contains "/etc/systemd/system/sing-box.service.d/limits.conf"
require_contains "configure_sing_box_systemd_limits"
require_contains '"level": "warn"'
require_contains '"tcp_fast_open": true'
require_contains '"tcp_multi_path": true'
require_contains '"connect_timeout": "10s"'

printf 'install-node renewal assertions passed
'
