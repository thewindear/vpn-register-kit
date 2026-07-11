#!/usr/bin/env bash
set -Eeuo pipefail

DRY_RUN=0
if [[ "${1:-}" == "--dry-run" ]]; then
  DRY_RUN=1
fi

WORK_DIR="/root/vpn-sub-kit"
NGINX_SITE="/etc/nginx/sites-available/vpn-fallback.conf"
NGINX_SITE_LINK="/etc/nginx/sites-enabled/vpn-fallback.conf"
SING_BOX_CONFIG="/etc/sing-box/config.json"
FALLBACK_PORT="8081"
TROJAN_PORT="443"
DEFAULT_SS_PORT="8080"
SS_METHOD="aes-128-gcm"

log() {
  printf '[INFO] %s\n' "$*"
}

warn() {
  printf '[WARN] %s\n' "$*" >&2
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

backup_file() {
  local path="$1"
  if [[ -e "$path" ]]; then
    local backup="${path}.bak-$(date +%Y%m%d-%H%M%S)"
    run_cmd cp "$path" "$backup"
    log "backed up $path to $backup"
  fi
}

prompt() {
  local label="$1"
  local default="${2:-}"
  local value
  if [[ -n "$default" ]]; then
    read -r -p "$label [$default]: " value
    printf '%s' "${value:-$default}"
  else
    read -r -p "$label: " value
    printf '%s' "$value"
  fi
}

prompt_secret() {
  local label="$1"
  local default="$2"
  local value
  read -r -s -p "$label [auto-generate]: " value
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

validate_node_id() {
  [[ "$1" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{1,63}$ ]]
}

validate_host() {
  [[ "$1" =~ ^[A-Za-z0-9][A-Za-z0-9.-]{1,252}[A-Za-z0-9]$ && "$1" == *.* ]]
}

validate_port() {
  [[ "$1" =~ ^[0-9]+$ ]] && (( "$1" >= 1 && "$1" <= 65535 ))
}

normalize_url() {
  local value="$1"
  value="${value%/}"
  printf '%s' "$value"
}

ensure_linux_root() {
  [[ "$(uname -s)" == "Linux" ]] || fail "This installer is intended for Ubuntu/Debian Linux servers only."
  [[ "$(id -u)" == "0" ]] || fail "Please run as root."
  if [[ ! -f /etc/debian_version ]]; then
    fail "Only Debian/Ubuntu-like systems are supported in version 1."
  fi
}

install_dependencies() {
  log "installing dependencies"
  run_cmd apt-get update
  run_cmd apt-get install -y curl jq nginx certbot python3-certbot-nginx ufw openssl ca-certificates
  if ! command -v sing-box >/dev/null 2>&1; then
    warn "sing-box is not installed by apt on every distro; attempting official package install"
    run_cmd bash -c "curl -fsSL https://sing-box.app/deb-install.sh | bash"
  fi
}

current_public_ip() {
  if [[ "$DRY_RUN" == "1" ]]; then
    printf ''
    return 0
  fi
  curl -fsS --max-time 5 https://api.ipify.org || true
}

check_dns() {
  local domain="$1"
  local public_ip="$2"
  local resolved
  resolved="$(getent ahostsv4 "$domain" | awk '{print $1; exit}' || true)"
  if [[ -z "$resolved" ]]; then
    warn "could not resolve $domain before certificate request"
    return 0
  fi
  if [[ -n "$public_ip" && "$resolved" != "$public_ip" ]]; then
    warn "$domain resolves to $resolved, current public IP appears to be $public_ip"
    warn "Let's Encrypt may fail until DNS points at this server."
  fi
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
  local domain="$1"
  local email="$2"
  if [[ -f "/etc/letsencrypt/live/$domain/fullchain.pem" && -f "/etc/letsencrypt/live/$domain/privkey.pem" ]]; then
    log "certificate already exists for $domain"
    return 0
  fi
  run_cmd certbot certonly --webroot -w "/var/www/$domain" -d "$domain" --email "$email" --agree-tos --non-interactive
}

configure_sing_box() {
  local domain="$1"
  local trojan_password="$2"
  local enable_ss="$3"
  local ss_port="$4"
  local ss_password="$5"
  backup_file "$SING_BOX_CONFIG"
  run_cmd mkdir -p /etc/sing-box

  local ss_block=""
  if [[ "$enable_ss" == "y" ]]; then
    ss_block=",
    {
      \"type\": \"shadowsocks\",
      \"listen\": \"::\",
      \"listen_port\": $ss_port,
      \"network\": \"tcp\",
      \"method\": \"$SS_METHOD\",
      \"password\": \"$ss_password\"
    }"
  fi

  write_file "$SING_BOX_CONFIG" "0600" <<EOF
{
  "log": {
    "level": "info"
  },
  "inbounds": [
    {
      "type": "trojan",
      "tag": "trojan-in",
      "listen": "::",
      "listen_port": $TROJAN_PORT,
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
      "tag": "direct"
    }
  ]
}
EOF
  run_cmd sing-box check -C /etc/sing-box
  run_cmd systemctl enable sing-box
  run_cmd systemctl restart sing-box
}

configure_firewall() {
  local enable_ss="$1"
  local ss_port="$2"
  run_cmd ufw allow 22/tcp
  run_cmd ufw allow 80/tcp
  run_cmd ufw allow 443/tcp
  if [[ "$enable_ss" == "y" ]]; then
    run_cmd ufw allow "$ss_port/tcp"
  fi
  run_cmd ufw --force enable
}

write_outputs() {
  local node_id="$1"
  local name="$2"
  local region="$3"
  local domain="$4"
  local public_ip="$5"
  local trojan_password="$6"
  local enable_ss="$7"
  local ss_port="$8"
  local ss_password="$9"
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

  local trojan_link
  trojan_link="trojan://$(python3 -c 'import sys, urllib.parse; print(urllib.parse.quote(sys.argv[1]))' "$trojan_password")@$domain:443?security=tls&sni=$domain&peer=$domain#$(python3 -c 'import sys, urllib.parse; print(urllib.parse.quote(sys.argv[1]))' "$name-Trojan")"
  local ss_link=""
  if [[ "$enable_ss" == "y" ]]; then
    local encoded
    encoded="$(printf '%s' "$SS_METHOD:$ss_password" | base64 | tr -d '\n' | tr '+/' '-_' | tr -d '=')"
    ss_link="ss://$encoded@$domain:$ss_port#$(python3 -c 'import sys, urllib.parse; print(urllib.parse.quote(sys.argv[1]))' "$name-SS")"
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
  local registry_url="$1"
  local token="$2"
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

main() {
  ensure_linux_root

  local node_id name region domain email trojan_password enable_ss ss_port ss_password register_choice registry_url register_token public_ip
  node_id="$(prompt "Node ID" "$(hostname)")"
  validate_node_id "$node_id" || fail "Invalid node_id. Use 2-64 chars: letters, numbers, dot, underscore, dash."
  name="$(prompt "Node name" "$node_id")"
  region="$(prompt "Region" "US")"
  domain="$(prompt "Node domain")"
  validate_host "$domain" || fail "Invalid domain."
  email="$(prompt "Let's Encrypt email")"
  [[ "$email" == *@* ]] || fail "Invalid email."
  trojan_password="$(prompt_secret "Trojan password" "$(random_secret)")"
  enable_ss="$(prompt "Enable Shadowsocks? y/N" "n")"
  enable_ss="$(printf '%s' "$enable_ss" | tr '[:upper:]' '[:lower:]')"
  if [[ "$enable_ss" == "y" ]]; then
    ss_port="$(prompt "Shadowsocks port" "$DEFAULT_SS_PORT")"
    validate_port "$ss_port" || fail "Invalid Shadowsocks port."
    ss_password="$(prompt_secret "Shadowsocks password" "$(random_secret)")"
  else
    ss_port="$DEFAULT_SS_PORT"
    ss_password=""
  fi
  register_choice="$(prompt "Register to subscription service? y/N" "n")"
  register_choice="$(printf '%s' "$register_choice" | tr '[:upper:]' '[:lower:]')"
  if [[ "$register_choice" == "y" ]]; then
    registry_url="$(prompt "Registry base URL, e.g. https://sub.example.com")"
    register_token="$(prompt_secret "Register token" "")"
    [[ -n "$registry_url" && -n "$register_token" ]] || fail "Registry URL and token are required when registration is enabled."
  else
    registry_url=""
    register_token=""
  fi

  public_ip="$(current_public_ip)"
  check_dns "$domain" "$public_ip"
  install_dependencies
  configure_nginx "$domain"
  obtain_certificate "$domain" "$email"
  configure_sing_box "$domain" "$trojan_password" "$enable_ss" "$ss_port" "$ss_password"
  configure_firewall "$enable_ss" "$ss_port"
  write_outputs "$node_id" "$name" "$region" "$domain" "$public_ip" "$trojan_password" "$enable_ss" "$ss_port" "$ss_password"
  verify_services
  if [[ "$register_choice" == "y" ]]; then
    register_node "$registry_url" "$register_token"
  fi
  log "done. Local outputs are in $WORK_DIR"
}

main "$@"
