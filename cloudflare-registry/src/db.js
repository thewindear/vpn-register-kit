import { hashToken, tokenHint } from "./auth.js";

export async function findEnabledToken(db, kind, token) {
  if (!token) return null;
  const tokenHash = await hashToken(token);
  const row = await db.prepare(
    "SELECT * FROM tokens WHERE token_hash = ? AND kind = ? AND enabled = 1"
  ).bind(tokenHash, kind).first();
  if (!row) return null;
  await db.prepare(
    "UPDATE tokens SET last_used_at = ? WHERE token_hash = ? AND kind = ?"
  ).bind(nowISO(), tokenHash, kind).run();
  return row;
}

export async function insertToken(db, { id, kind, token }) {
  await db.prepare(
    "INSERT INTO tokens (id, kind, token_hash, token_hint, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?)"
  ).bind(id, kind, await hashToken(token), tokenHint(token), 1, nowISO()).run();
}

export async function listTokens(db) {
  const result = await db.prepare(
    "SELECT id, kind, token_hint, enabled, created_at, last_used_at FROM tokens ORDER BY created_at ASC"
  ).all();
  return result.results || [];
}

export async function upsertNode(db, node) {
  const at = nowISO();
  await db.prepare(
    `INSERT INTO nodes (node_id, name, region, host, ip, protocols_json, schema_version, created_at, updated_at)
     VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
     ON CONFLICT(node_id) DO UPDATE SET
       name = excluded.name,
       region = excluded.region,
       host = excluded.host,
       ip = excluded.ip,
       protocols_json = excluded.protocols_json,
       schema_version = excluded.schema_version,
       updated_at = excluded.updated_at`
  ).bind(
    node.node_id,
    node.name,
    node.region || "",
    node.host,
    node.ip || "",
    JSON.stringify(node.protocols),
    node.schema_version,
    at,
    at
  ).run();
}

export async function listNodes(db) {
  const result = await db.prepare("SELECT * FROM nodes ORDER BY node_id ASC").all();
  return (result.results || []).map(rowToNode);
}

export async function deleteNode(db, nodeId) {
  const existing = await db.prepare("SELECT * FROM nodes WHERE node_id = ?").bind(nodeId).first();
  if (!existing) return false;
  await db.prepare("DELETE FROM nodes WHERE node_id = ?").bind(nodeId).run();
  return true;
}

export function rowToNode(row) {
  return {
    schema_version: row.schema_version,
    node_id: row.node_id,
    name: row.name,
    region: row.region || "",
    host: row.host,
    ip: row.ip || "",
    protocols: JSON.parse(row.protocols_json || "[]"),
    created_at: row.created_at,
    updated_at: row.updated_at
  };
}

export async function writeAudit(db, event) {
  try {
    await db.prepare(
      "INSERT INTO audit_logs (id, event_type, remote_ip, user_agent, token_hint, node_id, format, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)"
    ).bind(
      crypto.randomUUID(),
      event.event_type,
      event.remote_ip || "",
      event.user_agent || "",
      event.token_hint || "",
      event.node_id || "",
      event.format || "",
      event.status || 0,
      nowISO()
    ).run();
  } catch (error) {
    console.error("[ERROR] audit log failed", error?.message || error);
  }
}

export function nowISO() {
  return new Date().toISOString();
}
