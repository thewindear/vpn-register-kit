# Cloudflare Registry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Cloudflare Workers + D1 registry that is compatible with the existing node installer and subscription clients.

**Architecture:** The Worker routes requests, validates tokens, stores nodes and audit logs in D1, and renders subscription formats in JavaScript. Admin CLI scripts call the deployed Worker admin API instead of accessing D1 directly.

**Tech Stack:** Cloudflare Workers, D1, vanilla JavaScript modules, Node built-in test runner.

## Global Constraints

- Do not change the node installer registration contract.
- Do not require Cloudflare credentials for local tests.
- Do not store plaintext tokens in D1.
- Keep default `/sub?token=xxx` compatible with Shadowrocket/common link subscriptions.
- Keep implementation dependency-light; no runtime npm packages are required.

---

### Task 1: Project Structure And Tests

**Files:**
- Create: `cloudflare-registry/package.json`
- Create: `cloudflare-registry/src/*.js`
- Create: `cloudflare-registry/test/*.test.js`

**Interfaces:**
- Produces: local test harness for Worker route behavior and subscription renderers.

- [x] Write tests for register, subscribe, admin token creation, and node listing.
- [x] Verify tests fail before implementation.

### Task 2: Worker Implementation

**Files:**
- Create: `cloudflare-registry/src/index.js`
- Create: `cloudflare-registry/src/auth.js`
- Create: `cloudflare-registry/src/db.js`
- Create: `cloudflare-registry/src/renderers.js`
- Create: `cloudflare-registry/src/validators.js`
- Create: `cloudflare-registry/src/logs.js`

**Interfaces:**
- Consumes: D1 binding `DB`, secret `ADMIN_TOKEN`.
- Produces: `fetch(request, env)` Worker handler.

- [ ] Implement health, register, sub, api nodes, and admin routes.
- [ ] Implement SHA-256 token hashing and token hints.
- [ ] Implement D1 storage helpers.
- [ ] Implement renderers for shadowrocket, base64, clash, and sing-box.

### Task 3: Cloudflare Deployment Files

**Files:**
- Create: `cloudflare-registry/wrangler.toml`
- Create: `cloudflare-registry/migrations/0001_init.sql`
- Create: `cloudflare-registry/README.md`

**Interfaces:**
- Produces: documented Cloudflare deployment path.

- [ ] Add D1 migration.
- [ ] Add wrangler config template.
- [ ] Document create D1, run migrations, set secrets, deploy, and create tokens.

### Task 4: Admin CLI

**Files:**
- Create: `cloudflare-registry/scripts/admin-client.js`
- Create: `cloudflare-registry/scripts/create-token.js`
- Create: `cloudflare-registry/scripts/list-tokens.js`
- Create: `cloudflare-registry/scripts/list-nodes.js`

**Interfaces:**
- Consumes: `CF_REGISTRY_URL` and `CF_ADMIN_TOKEN`.
- Produces: local CLI commands for admin API calls.

- [ ] Implement token creation/listing and node listing scripts.
- [ ] Add package scripts.

### Task 5: Verification

**Files:**
- Modify: project docs as needed.

**Interfaces:**
- Produces: committed Cloudflare implementation.

- [ ] Run Cloudflare JS tests.
- [ ] Run existing Go registry tests.
- [ ] Commit implementation.
