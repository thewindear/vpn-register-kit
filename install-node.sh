#!/usr/bin/env bash
set -Eeuo pipefail

DRY_RUN=0
NON_INTERACTIVE=0
NODE_ID=""
NODE_NAME=""
REGION=""
DOMAIN=""
EMAIL=""
TROJAN_PASSWORD=""
ENABLE_SS=""
SS_PORT=""
SS_PASSWORD=""
REGISTER_CHOICE=""
REGISTRY_URL=""
REGISTER_TOKEN=""

WORK_DIR="/root/vpn-sub-kit"
SYSCTL_TUNING="/etc/sysctl.d/99-vpn-node-network-tuning.conf"
NGINX_SITE="/etc/nginx/sites-available/vpn-fallback.conf"
NGINX_SITE_LINK="/etc/nginx/sites-enabled/vpn-fallback.conf"
NGINX_SYSTEMD_LIMITS="/etc/systemd/system/nginx.service.d/limits.conf"
SING_BOX_CONFIG="/etc/sing-box/config.json"
SING_BOX_SYSTEMD_LIMITS="/etc/systemd/system/sing-box.service.d/limits.conf"
RENEWAL_HOOK="/etc/letsencrypt/renewal-hooks/deploy/reload-vpn-services"
FALLBACK_PORT="8081"
TROJAN_PORT="443"
DEFAULT_SS_PORT="8080"
SS_METHOD="aes-128-gcm"

usage() {
  cat <<'EOF'
Usage:
  bash install-node.sh [options]

Options:
  --dry-run
  --non-interactive
  --node-id VALUE
  --name VALUE
  --region VALUE
  --domain VALUE
  --email VALUE
  --trojan-password VALUE
  --enable-ss
  --disable-ss
  --ss-port VALUE
  --ss-password VALUE
  --registry-url VALUE
  --register-token VALUE
  --no-register
  -h, --help

Required for non-interactive mode:
  --node-id, --name, --region, --domain, --email

Registration is enabled only when both --registry-url and --register-token are provided.
EOF
}

log() { printf '[INFO] %s\n' "$*"; }
warn() { printf '[WARN] %s\n' "$*" >&2; }
fail() { printf '[ERROR] %s\n' "$*" >&2; exit 1; }

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --dry-run) DRY_RUN=1; shift ;;
      --non-interactive) NON_INTERACTIVE=1; shift ;;
      --node-id) NODE_ID="${2:-}"; shift 2 ;;
      --name) NODE_NAME="${2:-}"; shift 2 ;;
      --region) REGION="${2:-}"; shift 2 ;;
      --domain) DOMAIN="${2:-}"; shift 2 ;;
      --email) EMAIL="${2:-}"; shift 2 ;;
      --trojan-password) TROJAN_PASSWORD="${2:-}"; shift 2 ;;
      --enable-ss) ENABLE_SS="y"; shift ;;
      --disable-ss) ENABLE_SS="n"; shift ;;
      --ss-port) SS_PORT="${2:-}"; shift 2 ;;
      --ss-password) SS_PASSWORD="${2:-}"; shift 2 ;;
      --registry-url) REGISTRY_URL="${2:-}"; REGISTER_CHOICE="y"; shift 2 ;;
      --register-token) REGISTER_TOKEN="${2:-}"; REGISTER_CHOICE="y"; shift 2 ;;
      --no-register) REGISTER_CHOICE="n"; shift ;;
      -h|--help) usage; exit 0 ;;
      *) fail "Unknown option: $1" ;;
    esac
  done
}

run_cmd() {
  if [[ "$DRY_RUN" == "1" ]]; then
    printf '[DRY-RUN] %q' "$1"
    shift || true
    for arg in "$@"; do printf ' %q' "$arg"; done
    printf '\n'
    return 0
  fi
  "$@"
}

write_file() {
  local path="$1" mode="$2" tmp
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

backup_file() {
  local path="$1"
  if [[ -e "$path" ]]; then
    local backup="${path}.bak-$(date +%Y%m%d-%H%M%S)"
    run_cmd cp "$path" "$backup"
    log "backed up $path to $backup"
  fi
}

prompt() {
  local label="$1" default="${2:-}" value
  if [[ "$NON_INTERACTIVE" == "1" ]]; then
    [[ -n "$default" ]] || fail "$label is required in --non-interactive mode"
    printf '%s' "$default"
    return 0
  fi
  if [[ -n "$default" ]]; then
    read -r -p "$label [$default]: " value
    printf '%s' "${value:-$default}"
  else
    read -r -p "$label: " value
    printf '%s' "$value"
  fi
}

prompt_secret() {
  local label="$1" default="$2" value
  if [[ "$NON_INTERACTIVE" == "1" ]]; then
    [[ -n "$default" ]] || fail "$label is required in --non-interactive mode"
    printf '%s' "$default"
    return 0
  fi
  read -r -s -p "$label [auto-generate/reuse]: " value
  printf '\n' >&2
  printf '%s' "${value:-$default}"
}

random_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 24
  else
    tr -dc 'A-Za-z0-9' </dev/urandom | head -c 32
  fi
}

validate_node_id() { [[ "$1" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{1,63}$ ]]; }
validate_host() { [[ "$1" =~ ^[A-Za-z0-9][A-Za-z0-9.-]{1,252}[A-Za-z0-9]$ && "$1" == *.* ]]; }
validate_port() { [[ "$1" =~ ^[0-9]+$ ]] && (( "$1" >= 1 && "$1" <= 65535 )); }
normalize_url() { local value="$1"; value="${value%/}"; printf '%s' "$value"; }

ensure_linux_root() {
  [[ "$(uname -s)" == "Linux" ]] || fail "This installer is intended for Ubuntu/Debian Linux servers only."
  [[ "$(id -u)" == "0" ]] || fail "Please run as root."
  [[ -f /etc/debian_version ]] || fail "Only Debian/Ubuntu-like systems are supported in version 1."
}

install_dependencies() {
  log "installing dependencies"
  run_cmd apt-get update
  run_cmd apt-get install -y curl jq nginx certbot python3-certbot-nginx ufw openssl ca-certificates python3
  if ! command -v sing-box >/dev/null 2>&1; then
    warn "sing-box is not installed by apt on every distro; attempting official package install"
    run_cmd bash -c "curl -fsSL https://sing-box.app/deb-install.sh | bash"
  fi
}

patch_nginx_main_config() {
  local nginx_conf="/etc/nginx/nginx.conf"
  if [[ ! -f "$nginx_conf" ]]; then
    warn "$nginx_conf does not exist; skipping nginx global tuning"
    return 0
  fi
  backup_file "$nginx_conf"
  if [[ "$DRY_RUN" == "1" ]]; then
    log "would tune $nginx_conf for high concurrency"
    return 0
  fi
  python3 - <<'PY'
from pathlib import Path
import re

path = Path("/etc/nginx/nginx.conf")
text = path.read_text()

if not re.search(r"^[ \t]*worker_rlimit_nofile\b", text, re.MULTILINE):
    text = text.replace(
        "pid /run/nginx.pid;\n",
        "pid /run/nginx.pid;\nworker_rlimit_nofile 200000;\n",
        1,
    )

text = re.sub(r"worker_connections\s+\d+;", "worker_connections 8192;", text, count=1)
text = text.replace("# multi_accept on;", "multi_accept on;")

if not re.search(r"^[ \t]*use\s+epoll;", text, re.MULTILINE):
    text = text.replace("events {\n", "events {\n\tuse epoll;\n", 1)

has_keepalive_timeout = re.search(r"^[ \t]*keepalive_timeout\b.*$", text, re.MULTILINE)
has_keepalive_requests = re.search(r"^[ \t]*keepalive_requests\b", text, re.MULTILINE)
if not has_keepalive_timeout:
    text = text.replace(
        "\t# Basic Settings\n\t##\n",
        "\t# Basic Settings\n\t##\n\tkeepalive_timeout 15;\n\tkeepalive_requests 1000;\n",
        1,
    )
elif not has_keepalive_requests:
    text = text.replace(
        has_keepalive_timeout.group(0) + "\n",
        has_keepalive_timeout.group(0) + "\n\tkeepalive_requests 1000;\n",
        1,
    )

path.write_text(text)
PY
}

configure_linux_network_tuning() {
  log "configuring Linux TCP and nginx concurrency tuning"
  backup_file "$SYSCTL_TUNING"
  write_file "$SYSCTL_TUNING" "0644" <<'EOF'
# Increase TCP accept queues and connection churn capacity for HTTP/TCP services.
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
net.ipv4.tcp_synack_retries = 3
net.ipv4.ip_local_port_range = 10000 65000
net.ipv4.tcp_fin_timeout = 15
net.ipv4.tcp_max_tw_buckets = 2000000
net.ipv4.tcp_tw_reuse = 2
net.ipv4.tcp_fastopen = 3
EOF

  backup_file "$NGINX_SYSTEMD_LIMITS"
  write_file "$NGINX_SYSTEMD_LIMITS" "0644" <<'EOF'
[Service]
LimitNOFILE=524288
TasksMax=infinity
EOF

  patch_nginx_main_config
  run_cmd sysctl --system
  run_cmd systemctl daemon-reload
}

current_public_ip() {
  if [[ "$DRY_RUN" == "1" ]]; then printf ''; return 0; fi
  curl -fsS --max-time 5 https://api.ipify.org || true
}

check_dns() {
  local domain="$1" public_ip="$2" resolved
  resolved="$(getent ahostsv4 "$domain" | awk '{print $1; exit}' || true)"
  if [[ -z "$resolved" ]]; then
    warn "could not resolve $domain before certificate request"
    return 0
  fi
  if [[ -n "$public_ip" && "$resolved" != "$public_ip" ]]; then
    fail "$domain resolves to $resolved, current public IP appears to be $public_ip. Fix DNS first and make sure this node hostname is DNS-only/direct."
  fi
}

existing_protocol_value() {
  local protocol="$1" field="$2" file="$WORK_DIR/node.json"
  if [[ -f "$file" ]] && command -v jq >/dev/null 2>&1; then
    jq -r --arg type "$protocol" --arg field "$field" '.protocols[]? | select(.type == $type) | .[$field] // empty' "$file" 2>/dev/null | head -n 1
  fi
}

configure_firewall() {
  local enable_ss="$1" ss_port="$2"
  run_cmd ufw allow 22/tcp
  run_cmd ufw allow 80/tcp
  run_cmd ufw allow 443/tcp
  if [[ "$enable_ss" == "y" ]]; then run_cmd ufw allow "$ss_port/tcp"; fi
  run_cmd ufw --force enable
}

configure_nginx() {
  local domain="$1"
  backup_file "$NGINX_SITE"
  write_file "$NGINX_SITE" "0644" <<EOF
server {
    listen 80;
    listen [::]:80;
    server_name $domain;

    root /var/www/$domain;
    index index.html;

    location / {
        try_files \$uri \$uri/ =404;
    }
}

server {
    listen 127.0.0.1:$FALLBACK_PORT;
    server_name $domain;

    root /var/www/$domain;
    index index.html;

    location / {
        try_files \$uri \$uri/ =404;
    }
}
EOF
  run_cmd mkdir -p "/var/www/$domain"
  write_file "/var/www/$domain/index.html" "0644" <<EOF
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>$domain</title>
</head>
<body>
  <h1>$domain</h1>
  <p>Service is running.</p>
</body>
</html>
EOF
  run_cmd ln -sf "$NGINX_SITE" "$NGINX_SITE_LINK"
  run_cmd nginx -t
  run_cmd systemctl enable nginx
  run_cmd systemctl restart nginx
}

obtain_certificate() {
  local domain="$1" email="$2"
  if [[ -f "/etc/letsencrypt/live/$domain/fullchain.pem" && -f "/etc/letsencrypt/live/$domain/privkey.pem" ]]; then
    log "certificate already exists for $domain"
    return 0
  fi
  run_cmd certbot certonly --webroot -w "/var/www/$domain" -d "$domain" --email "$email" --agree-tos --non-interactive
}

configure_certificate_renewal() {
  run_cmd mkdir -p "$(dirname "$RENEWAL_HOOK")"
  write_file "$RENEWAL_HOOK" "0755" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail

systemctl restart sing-box
systemctl reload nginx || systemctl restart nginx
EOF
  run_cmd systemctl enable certbot.timer
  run_cmd systemctl start certbot.timer
}

configure_sing_box_systemd_limits() {
  backup_file "$SING_BOX_SYSTEMD_LIMITS"
  write_file "$SING_BOX_SYSTEMD_LIMITS" "0644" <<'EOF'
[Service]
LimitNOFILE=infinity
TasksMax=infinity
EOF
  run_cmd systemctl daemon-reload
}

configure_sing_box() {
  local domain="$1" trojan_password="$2" enable_ss="$3" ss_port="$4" ss_password="$5"
  backup_file "$SING_BOX_CONFIG"
  run_cmd mkdir -p /etc/sing-box
  configure_sing_box_systemd_limits

  local ss_block=""
  if [[ "$enable_ss" == "y" ]]; then
    ss_block=",
    {
      \"type\": \"shadowsocks\",
      \"listen\": \"::\",
      \"listen_port\": $ss_port,
      \"network\": \"tcp\",
      \"tcp_fast_open\": true,
      \"tcp_multi_path\": true,
      \"method\": \"$SS_METHOD\",
      \"password\": \"$ss_password\"
    }"
  fi

  write_file "$SING_BOX_CONFIG" "0600" <<EOF
{
  "log": {
    "level": "warn"
  },
  "inbounds": [
    {
      "type": "trojan",
      "tag": "trojan-in",
      "listen": "::",
      "listen_port": $TROJAN_PORT,
      "tcp_fast_open": true,
      "tcp_multi_path": true,
      "users": [
        {
          "password": "$trojan_password"
        }
      ],
      "tls": {
        "enabled": true,
        "server_name": "$domain",
        "certificate_path": "/etc/letsencrypt/live/$domain/fullchain.pem",
        "key_path": "/etc/letsencrypt/live/$domain/privkey.pem"
      },
      "fallback": {
        "server": "127.0.0.1",
        "server_port": $FALLBACK_PORT
      }
    }$ss_block
  ],
  "outbounds": [
    {
      "type": "direct",
      "tag": "direct",
      "tcp_fast_open": true,
      "tcp_multi_path": true,
      "connect_timeout": "10s"
    }
  ]
}
EOF
  run_cmd sing-box check -C /etc/sing-box
  run_cmd systemctl enable sing-box
  run_cmd systemctl restart sing-box
}

url_quote() {
  python3 -c 'import sys, urllib.parse; print(urllib.parse.quote(sys.argv[1]))' "$1"
}

write_outputs() {
  local node_id="$1" name="$2" region="$3" domain="$4" public_ip="$5" trojan_password="$6" enable_ss="$7" ss_port="$8" ss_password="$9"
  run_cmd mkdir -p "$WORK_DIR"

  local protocols_json
  protocols_json="[
    {
      \"type\": \"trojan\",
      \"port\": 443,
      \"password\": \"$trojan_password\",
      \"sni\": \"$domain\",
      \"tls\": true
    }"
  if [[ "$enable_ss" == "y" ]]; then
    protocols_json="$protocols_json,
    {
      \"type\": \"shadowsocks\",
      \"port\": $ss_port,
      \"method\": \"$SS_METHOD\",
      \"password\": \"$ss_password\"
    }"
  fi
  protocols_json="$protocols_json
  ]"

  write_file "$WORK_DIR/node.json" "0600" <<EOF
{
  "schema_version": 1,
  "node_id": "$node_id",
  "name": "$name",
  "region": "$region",
  "host": "$domain",
  "ip": "$public_ip",
  "protocols": $protocols_json
}
EOF

  local trojan_link ss_link=""
  trojan_link="trojan://$(url_quote "$trojan_password")@$domain:443?security=tls&sni=$domain&peer=$domain#$(url_quote "$name-Trojan")"
  if [[ "$enable_ss" == "y" ]]; then
    local encoded
    encoded="$(printf '%s' "$SS_METHOD:$ss_password" | base64 | tr -d '\n' | tr '+/' '-_' | tr -d '=')"
    ss_link="ss://$encoded@$domain:$ss_port#$(url_quote "$name-SS")"
  fi
  write_file "$WORK_DIR/shadowrocket.txt" "0600" <<EOF
$trojan_link
$ss_link
EOF

  write_file "$WORK_DIR/clash.yaml" "0600" <<EOF
mixed-port: 7890
allow-lan: false
mode: rule
log-level: info
proxies:
  - name: "$name-Trojan"
    type: trojan
    server: "$domain"
    port: 443
    password: "$trojan_password"
    sni: "$domain"
    skip-cert-verify: false
    udp: true
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - "$name-Trojan"
      - DIRECT
rules:
  - GEOIP,CN,DIRECT
  - MATCH,PROXY
EOF

  write_file "$WORK_DIR/sing-box.json" "0600" <<EOF
{
  "log": {"level": "info"},
  "outbounds": [
    {
      "type": "trojan",
      "tag": "$name-Trojan",
      "server": "$domain",
      "server_port": 443,
      "password": "$trojan_password",
      "tls": {
        "enabled": true,
        "server_name": "$domain"
      }
    },
    {
      "type": "direct",
      "tag": "direct"
    }
  ],
  "route": {
    "final": "direct"
  }
}
EOF
}

register_node() {
  local registry_url="$1" token="$2"
  registry_url="$(normalize_url "$registry_url")"
  run_cmd curl -fsS -X POST "$registry_url/register" \
    -H "Authorization: Bearer $token" \
    -H "Content-Type: application/json" \
    --data-binary "@$WORK_DIR/node.json"
}

verify_services() {
  run_cmd systemctl is-active nginx
  run_cmd systemctl is-active sing-box
  run_cmd ss -tulpen
}

require_value() {
  local label="$1" value="$2"
  [[ -n "$value" ]] || fail "$label is required"
}

main() {
  parse_args "$@"
  ensure_linux_root

  local existing_trojan existing_ss
  existing_trojan="$(existing_protocol_value trojan password || true)"
  existing_ss="$(existing_protocol_value shadowsocks password || true)"

  NODE_ID="$(prompt "Node ID" "${NODE_ID:-$(hostname)}")"
  validate_node_id "$NODE_ID" || fail "Invalid node_id. Use 2-64 chars: letters, numbers, dot, underscore, dash."
  NODE_NAME="$(prompt "Node name" "${NODE_NAME:-$NODE_ID}")"
  REGION="$(prompt "Region" "${REGION:-}")"
  DOMAIN="$(prompt "Node domain" "${DOMAIN:-}")"
  validate_host "$DOMAIN" || fail "Invalid domain."
  EMAIL="$(prompt "Let's Encrypt email" "${EMAIL:-}")"
  [[ "$EMAIL" == *@* ]] || fail "Invalid email."

  TROJAN_PASSWORD="$(prompt_secret "Trojan password" "${TROJAN_PASSWORD:-${existing_trojan:-$(random_secret)}}")"
  ENABLE_SS="$(prompt "Enable Shadowsocks? y/N" "${ENABLE_SS:-n}")"
  ENABLE_SS="$(printf '%s' "$ENABLE_SS" | tr '[:upper:]' '[:lower:]')"
  if [[ "$ENABLE_SS" == "y" ]]; then
    SS_PORT="$(prompt "Shadowsocks port" "${SS_PORT:-$DEFAULT_SS_PORT}")"
    validate_port "$SS_PORT" || fail "Invalid Shadowsocks port."
    SS_PASSWORD="$(prompt_secret "Shadowsocks password" "${SS_PASSWORD:-${existing_ss:-$(random_secret)}}")"
  else
    SS_PORT="${SS_PORT:-$DEFAULT_SS_PORT}"
    SS_PASSWORD=""
  fi

  if [[ -n "$REGISTRY_URL" || -n "$REGISTER_TOKEN" ]]; then
    REGISTER_CHOICE="y"
  fi
  REGISTER_CHOICE="$(prompt "Register to subscription service? y/N" "${REGISTER_CHOICE:-n}")"
  REGISTER_CHOICE="$(printf '%s' "$REGISTER_CHOICE" | tr '[:upper:]' '[:lower:]')"
  if [[ "$REGISTER_CHOICE" == "y" ]]; then
    REGISTRY_URL="$(prompt "Registry base URL, e.g. https://sub.example.com" "${REGISTRY_URL:-}")"
    REGISTER_TOKEN="$(prompt_secret "Register token" "${REGISTER_TOKEN:-}")"
    require_value "Registry URL" "$REGISTRY_URL"
    require_value "Register token" "$REGISTER_TOKEN"
  else
    REGISTRY_URL=""
    REGISTER_TOKEN=""
  fi

  local public_ip
  public_ip="$(current_public_ip)"
  check_dns "$DOMAIN" "$public_ip"
  install_dependencies
  configure_linux_network_tuning
  configure_firewall "$ENABLE_SS" "$SS_PORT"
  configure_nginx "$DOMAIN"
  obtain_certificate "$DOMAIN" "$EMAIL"
  configure_certificate_renewal
  configure_sing_box "$DOMAIN" "$TROJAN_PASSWORD" "$ENABLE_SS" "$SS_PORT" "$SS_PASSWORD"
  write_outputs "$NODE_ID" "$NODE_NAME" "$REGION" "$DOMAIN" "$public_ip" "$TROJAN_PASSWORD" "$ENABLE_SS" "$SS_PORT" "$SS_PASSWORD"
  verify_services
  if [[ "$REGISTER_CHOICE" == "y" ]]; then
    register_node "$REGISTRY_URL" "$REGISTER_TOKEN"
  fi
  log "done. Local outputs are in $WORK_DIR"
}

main "$@"
