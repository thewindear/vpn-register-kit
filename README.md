# VPN Register Kit

Lightweight tools for installing VPN nodes and publishing them through one subscription URL.

Version 1 includes:

- A shell node installer for Ubuntu/Debian servers.
- A Go registry service for node registration and subscription rendering.
- A Cloudflare Workers + D1 registry service with the same HTTP contract.
- Registry CLI commands for token and node inspection.
- Subscription formats for Shadowrocket, Clash/Mihomo, sing-box, and base64 link lists.

## Layout

```text
install-node.sh          # run on each VPN node server
install-go-registry.sh   # optional Go registry systemd installer
go-registry/             # Go registry service and CLI
cloudflare-registry/     # Cloudflare Workers + D1 registry
docs/requirements.md
```

## Build The Go Registry

```bash
cd go-registry
go build -o ../vpn-registry .
```

## Start The Go Registry

First start creates `config.json` with one register token and two subscribe tokens:

```bash
./vpn-registry server -config ./config.json
```

The generated config looks like:

```json
{
  "listen": ":8088",
  "public_base_url": "http://127.0.0.1:8088",
  "register_tokens": ["..."],
  "subscribe_tokens": ["...", "..."],
  "data_file": "./nodes.json"
}
```

Set `public_base_url` to your public registry URL, for example:

```json
"public_base_url": "https://sub.example.com"
```

## Registry CLI

```bash
./vpn-registry token list -config ./config.json
./vpn-registry token create -config ./config.json
./vpn-registry token delete -config ./config.json -token xxx

./vpn-registry node list -config ./config.json
./vpn-registry node show -config ./config.json -id us-la-001
```

`token create` prints the full new subscribe token once. Other commands mask tokens and protocol passwords.

## Registry HTTP API

Health:

```bash
curl http://127.0.0.1:8088/health
```

Register a node:

```bash
curl -X POST https://sub.example.com/register \
  -H "Authorization: Bearer REGISTER_TOKEN" \
  -H "Content-Type: application/json" \
  --data-binary @node.json
```

List registered node summaries:

```bash
curl https://sub.example.com/api/nodes \
  -H "Authorization: Bearer REGISTER_TOKEN"
```

Subscription URLs:

```text
https://sub.example.com/sub?token=SUB_TOKEN
https://sub.example.com/sub?token=SUB_TOKEN&format=shadowrocket
https://sub.example.com/sub?token=SUB_TOKEN&format=base64
https://sub.example.com/sub?token=SUB_TOKEN&format=clash
https://sub.example.com/sub?token=SUB_TOKEN&format=sing-box
```

## Client Formats

| Client | URL format |
|---|---|
| Shadowrocket | `/sub?token=SUB_TOKEN` |
| v2rayN / v2rayNG | `/sub?token=SUB_TOKEN&format=base64` |
| ClashX / Clash Verge / Mihomo Party / Stash | `/sub?token=SUB_TOKEN&format=clash` |
| sing-box | `/sub?token=SUB_TOKEN&format=sing-box` |

## Install A Node

Copy the project or just `install-node.sh` to a new Ubuntu/Debian server, then run:

```bash
bash install-node.sh
```

To preview actions on a Linux server without installing packages or writing system config:

```bash
bash install-node.sh --dry-run
```

Interactive mode asks for:

- Node ID, name, region, and domain.
- Let's Encrypt email.
- Trojan password, generated or reused by default.
- Optional Shadowsocks settings.
- Optional registry URL and register token.

Non-interactive mode is available for repeatable installs:

```bash
bash install-node.sh \
  --non-interactive \
  --node-id sg01 \
  --name SG-01 \
  --region SG \
  --domain sg01.005700.xyz \
  --email admin@example.com \
  --enable-ss \
  --registry-url https://sub.example.com \
  --register-token REGISTER_TOKEN
```

The installer opens UFW ports before certificate issuance, so Let's Encrypt can reach `80/tcp`. If `/root/vpn-sub-kit/node.json` already exists, existing Trojan and Shadowsocks passwords are reused by default unless explicitly supplied.

Local outputs are written to:

```text
/root/vpn-sub-kit/node.json
/root/vpn-sub-kit/shadowrocket.txt
/root/vpn-sub-kit/clash.yaml
/root/vpn-sub-kit/sing-box.json
```

## Firewall Ports

Open these inbound ports on the cloud firewall for a node:

| Port | Protocol | Purpose |
|---|---|---|
| `22` | TCP | SSH |
| `80` | TCP | nginx and Let's Encrypt HTTP validation |
| `443` | TCP | Trojan over TLS |
| `8080` | TCP | Optional Shadowsocks |

For the registry service, expose the configured `listen` port behind your own HTTPS reverse proxy.

## Install Registry As A Service

On a Linux registry host with Go installed:

```bash
bash install-go-registry.sh
```

Dry-run:

```bash
bash install-go-registry.sh --dry-run
```

Logs:

```bash
journalctl -u vpn-registry -f
```

## Notes

- The registry stores config and nodes as JSON files.
- `config.json` and `nodes.json` are written with `0600` permissions.
- Logs do not print full tokens or protocol passwords.
- Version 1 does not include a web dashboard, database, traffic stats, node groups, active health checks, or subscription expiry.

## Cloudflare Registry

The Cloudflare implementation lives in `cloudflare-registry/` and uses Workers + D1. It accepts the same `/register` payload as the Go registry, so `install-node.sh` can report to either backend by changing only the registry URL.

See [cloudflare-registry/README.md](cloudflare-registry/README.md).
