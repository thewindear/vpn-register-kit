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

printf 'install-node renewal assertions passed
'
