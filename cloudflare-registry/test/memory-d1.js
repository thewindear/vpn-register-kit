export class MemoryD1 {
  constructor() {
    this.nodes = new Map();
    this.tokens = [];
    this.auditLogs = [];
  }

  prepare(sql) {
    return new MemoryStatement(this, sql);
  }
}

class MemoryStatement {
  constructor(db, sql) {
    this.db = db;
    this.sql = sql.replace(/\s+/g, " ").trim();
    this.params = [];
  }

  bind(...params) {
    this.params = params;
    return this;
  }

  async first() {
    const rows = await this._rows();
    return rows[0] || null;
  }

  async all() {
    return { results: await this._rows() };
  }

  async run() {
    const sql = this.sql;
    if (sql.startsWith("INSERT INTO tokens")) {
      const [id, kind, tokenHash, tokenHint, enabled, createdAt] = this.params;
      this.db.tokens.push({ id, kind, token_hash: tokenHash, token_hint: tokenHint, enabled, created_at: createdAt, last_used_at: null });
      return { success: true };
    }
    if (sql.startsWith("UPDATE tokens SET last_used_at")) {
      const [lastUsedAt, tokenHash, kind] = this.params;
      const token = this.db.tokens.find((item) => item.token_hash === tokenHash && item.kind === kind);
      if (token) token.last_used_at = lastUsedAt;
      return { success: true };
    }
    if (sql.startsWith("INSERT INTO nodes")) {
      const [nodeId, name, region, host, ip, protocolsJson, schemaVersion, createdAt, updatedAt] = this.params;
      const existing = this.db.nodes.get(nodeId);
      this.db.nodes.set(nodeId, {
        node_id: nodeId,
        name,
        region,
        host,
        ip,
        protocols_json: protocolsJson,
        schema_version: schemaVersion,
        created_at: existing?.created_at || createdAt,
        updated_at: updatedAt
      });
      return { success: true };
    }
    if (sql.startsWith("DELETE FROM nodes WHERE node_id")) {
      const [nodeId] = this.params;
      this.db.nodes.delete(nodeId);
      return { success: true };
    }
    if (sql.startsWith("INSERT INTO audit_logs")) {
      const [id, eventType, remoteIp, userAgent, tokenHint, nodeId, format, status, createdAt] = this.params;
      this.db.auditLogs.push({ id, event_type: eventType, remote_ip: remoteIp, user_agent: userAgent, token_hint: tokenHint, node_id: nodeId, format, status, created_at: createdAt });
      return { success: true };
    }
    throw new Error(`unsupported run SQL: ${sql}`);
  }

  async _rows() {
    const sql = this.sql;
    if (sql.startsWith("SELECT id, kind, token_hint")) {
      return [...this.db.tokens].sort((a, b) => a.created_at.localeCompare(b.created_at));
    }
    if (sql.startsWith("SELECT * FROM tokens WHERE token_hash")) {
      const [tokenHash, kind] = this.params;
      return this.db.tokens.filter((item) => item.token_hash === tokenHash && item.kind === kind && item.enabled === 1);
    }
    if (sql.startsWith("SELECT * FROM nodes ORDER BY node_id")) {
      return [...this.db.nodes.values()].sort((a, b) => a.node_id.localeCompare(b.node_id));
    }
    if (sql.startsWith("SELECT * FROM nodes WHERE node_id")) {
      const [nodeId] = this.params;
      const node = this.db.nodes.get(nodeId);
      return node ? [node] : [];
    }
    throw new Error(`unsupported query SQL: ${sql}`);
  }
}
