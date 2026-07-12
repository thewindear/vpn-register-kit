# Cloudflare Registry

Cloudflare Workers + D1 implementation of the VPN Register Kit registry.

It is compatible with the existing node installer:

```text
POST /register
Authorization: Bearer <register_token>
```

The same `install-node.sh` can report nodes to the Go registry or this Cloudflare Worker.

## Files

```text
src/                  Worker source
migrations/           D1 SQL migrations
scripts/              Admin CLI scripts
wrangler.toml         Cloudflare config template
test/                 Local Worker tests with an in-memory D1 adapter
```

## Local Tests

No Cloudflare token is required for tests:

```bash
node --test
```

## Cloudflare Setup

Install dependencies if needed:

```bash
pnpm install
```

Interactive setup:

```bash
pnpm setup:interactive
```

The interactive script can:

- check `wrangler login`
- create the D1 database
- update `wrangler.toml` with `database_id`
- generate and upload `ADMIN_TOKEN`
- apply migrations
- deploy the Worker
- create initial register and subscribe tokens

Manual setup is also supported:

Create D1:

```bash
wrangler d1 create vpn-register-kit
```

Copy the returned `database_id` into `wrangler.toml`.

Apply migrations:

```bash
wrangler d1 migrations apply DB --remote
```

Set admin secret:

```bash
wrangler secret put ADMIN_TOKEN
```

Deploy:

```bash
wrangler deploy
```

## Create Tokens

After deployment, set local admin environment variables:

```bash
export CF_REGISTRY_URL=https://sub.example.com
export CF_ADMIN_TOKEN=your-admin-token
```

Create a register token:

```bash
pnpm cf:token:create -- --kind register
```

Create a subscribe token:

```bash
pnpm cf:token:create -- --kind subscribe
```

List token hints:

```bash
pnpm cf:token:list
```

List nodes:

```bash
pnpm cf:node:list
```

## Client Subscription URLs

Default Shadowrocket/common links:

```text
https://sub.example.com/sub?token=SUB_TOKEN
```

Clash/Mihomo:

```text
https://sub.example.com/sub?token=SUB_TOKEN&format=clash
```

sing-box:

```text
https://sub.example.com/sub?token=SUB_TOKEN&format=sing-box
```

base64 links:

```text
https://sub.example.com/sub?token=SUB_TOKEN&format=base64
```

## Security Notes

- D1 stores token hashes and hints, not plaintext tokens.
- Full tokens are returned only once when created.
- Protocol passwords are stored in D1 because subscription rendering requires them.
- Logs and admin node listings do not print protocol passwords.
