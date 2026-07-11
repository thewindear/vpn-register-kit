# Cloudflare Registry Design

## Goal

Add a Cloudflare Workers + D1 registry implementation that is wire-compatible with the existing Go registry. Existing node installers continue to submit the same `POST /register` request to either backend.

## Compatibility Contract

The Cloudflare Worker must accept the same node payload and subscription URLs as the Go registry:

- `POST /register` with `Authorization: Bearer <register_token>`
- `GET /sub?token=<subscribe_token>`
- `GET /sub?token=<subscribe_token>&format=shadowrocket`
- `GET /sub?token=<subscribe_token>&format=base64`
- `GET /sub?token=<subscribe_token>&format=clash`
- `GET /sub?token=<subscribe_token>&format=sing-box`
- `GET /api/nodes` with `Authorization: Bearer <register_or_admin_token>`

The node installer must not need to know whether the registry URL points to Go or Cloudflare.

## Cloudflare Components

- Cloudflare Worker handles HTTP routes.
- Cloudflare D1 stores nodes, tokens, and audit logs.
- `ADMIN_TOKEN` is a Worker secret used for admin endpoints.
- Token rows store SHA-256 hashes, not plaintext tokens.

## D1 Schema

- `nodes`: one row per `node_id`, with `protocols_json` storing protocol details.
- `tokens`: register and subscribe tokens as hashes plus short hints.
- `audit_logs`: best-effort request and admin event logs.

## Admin API

- `POST /admin/tokens/create` creates a register or subscribe token and returns the plaintext token once.
- `GET /admin/tokens` lists token hints and metadata.
- `GET /admin/nodes` lists registered node summaries.

Admin endpoints require `Authorization: Bearer <ADMIN_TOKEN>`.

## Local CLI

The JS CLI scripts call the deployed Worker admin API:

- `pnpm cf:token:create -- --kind subscribe`
- `pnpm cf:token:list`
- `pnpm cf:node:list`

They use:

- `CF_REGISTRY_URL`
- `CF_ADMIN_TOKEN`

## Out Of Scope

- Cloudflare account provisioning from code.
- Automatic D1 creation without `wrangler`.
- Web dashboard.
- Node groups, expiry, traffic stats, and active probing.
