import { bearerToken, hashToken, randomToken, requireAdmin, tokenHint } from "./auth.js";
import { deleteNode, findEnabledToken, insertToken, listNodes, listTokens, upsertNode, writeAudit } from "./db.js";
import { logInfo, logWarn, requestMeta } from "./logs.js";
import { renderSubscription } from "./renderers.js";
import { validateNode } from "./validators.js";

const MAX_REGISTER_BYTES = 64 * 1024;

export default {
  async fetch(request, env) {
    try {
      return await route(request, env);
    } catch (error) {
      console.error("[ERROR] unhandled", error?.stack || error);
      return json({ error: "internal error" }, 500);
    }
  }
};

async function route(request, env) {
  const url = new URL(request.url);
  if (url.pathname === "/health" && request.method === "GET") {
    return json({ status: "ok" });
  }
  if (url.pathname === "/register") return handleRegister(request, env);
  if (url.pathname === "/sub") return handleSub(request, env);
  if (url.pathname === "/api/nodes") return handleApiNodes(request, env);
  if (url.pathname.startsWith("/api/nodes/")) return handleApiNode(request, env);
  if (url.pathname === "/admin/tokens/create") return handleAdminTokenCreate(request, env);
  if (url.pathname === "/admin/tokens") return handleAdminTokens(request, env);
  if (url.pathname === "/admin/nodes") return handleAdminNodes(request, env);
  if (url.pathname.startsWith("/admin/nodes/")) return handleAdminNode(request, env);
  return text("not found", 404);
}

async function handleRegister(request, env) {
  if (request.method !== "POST") return text("method not allowed", 405);
  const meta = requestMeta(request);
  const token = bearerToken(request);
  const row = await findEnabledToken(env.DB, "register", token);
  if (!row) {
    logWarn("register denied", { remote_ip: meta.remote_ip, token: tokenHint(token), reason: "invalid_token" });
    await writeAudit(env.DB, { event_type: "register_denied", ...meta, token_hint: tokenHint(token), status: 401 });
    return text("unauthorized", 401);
  }
  const body = await readLimitedJson(request, MAX_REGISTER_BYTES);
  try {
    validateNode(body);
  } catch (error) {
    await writeAudit(env.DB, { event_type: "register_invalid", ...meta, token_hint: row.token_hint, node_id: body?.node_id || "", status: 400 });
    return text(error.message, 400);
  }
  await upsertNode(env.DB, body);
  await writeAudit(env.DB, { event_type: "register_node", ...meta, token_hint: row.token_hint, node_id: body.node_id, status: 200 });
  logInfo("node registered", { remote_ip: meta.remote_ip, node_id: body.node_id, host: body.host, protocols: (body.protocols || []).map((p) => p.type).join(",") });
  return json({ status: "ok" });
}

async function handleSub(request, env) {
  if (request.method !== "GET") return text("method not allowed", 405);
  const url = new URL(request.url);
  const meta = requestMeta(request);
  const token = url.searchParams.get("token") || "";
  const tokenRow = await findEnabledToken(env.DB, "subscribe", token);
  if (!tokenRow) {
    logWarn("sub denied", { remote_ip: meta.remote_ip, token: tokenHint(token), reason: "invalid_token" });
    await writeAudit(env.DB, { event_type: "sub_denied", ...meta, token_hint: tokenHint(token), format: url.searchParams.get("format") || "shadowrocket", status: 401 });
    return text("unauthorized", 401);
  }
  const format = url.searchParams.get("format") || "shadowrocket";
  const nodes = await listNodes(env.DB);
  let rendered;
  try {
    rendered = renderSubscription(format, nodes);
  } catch (error) {
    return text(error.message, 400);
  }
  await writeAudit(env.DB, { event_type: "sub_request", ...meta, token_hint: tokenRow.token_hint, format, status: 200 });
  logInfo("sub request", { remote_ip: meta.remote_ip, token: tokenRow.token_hint, format, nodes: nodes.length });
  return new Response(rendered.body, {
    status: 200,
    headers: { "content-type": rendered.contentType }
  });
}

async function handleApiNodes(request, env) {
  if (request.method !== "GET") return text("method not allowed", 405);
  const token = bearerToken(request);
  const registerToken = await findEnabledToken(env.DB, "register", token);
  const isAdmin = requireAdmin(request, env);
  if (!registerToken && !isAdmin) return text("unauthorized", 401);
  const nodes = await listNodes(env.DB);
  return json({ nodes: nodes.map(nodeSummary) });
}

async function handleAdminNodes(request, env) {
  if (request.method !== "GET") return text("method not allowed", 405);
  if (!requireAdmin(request, env)) return text("unauthorized", 401);
  const nodes = await listNodes(env.DB);
  return json({ nodes: nodes.map(nodeSummary) });
}

async function handleApiNode(request, env) {
  if (request.method !== "DELETE") return text("method not allowed", 405);
  const token = bearerToken(request);
  const registerToken = await findEnabledToken(env.DB, "register", token);
  const isAdmin = requireAdmin(request, env);
  if (!registerToken && !isAdmin) return text("unauthorized", 401);
  return deleteNodeFromPath(request, env, "/api/nodes/");
}

async function handleAdminNode(request, env) {
  if (request.method !== "DELETE") return text("method not allowed", 405);
  if (!requireAdmin(request, env)) return text("unauthorized", 401);
  return deleteNodeFromPath(request, env, "/admin/nodes/");
}

async function deleteNodeFromPath(request, env, prefix) {
  const url = new URL(request.url);
  const nodeId = decodeURIComponent(url.pathname.slice(prefix.length));
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]{1,63}$/.test(nodeId)) return text("invalid node_id", 400);
  const deleted = await deleteNode(env.DB, nodeId);
  const meta = requestMeta(request);
  if (!deleted) {
    await writeAudit(env.DB, { event_type: "node_delete_missing", ...meta, node_id: nodeId, status: 404 });
    return text("node not found", 404);
  }
  await writeAudit(env.DB, { event_type: "node_delete", ...meta, node_id: nodeId, status: 200 });
  logInfo("node deleted", { remote_ip: meta.remote_ip, node_id: nodeId });
  return json({ status: "ok" });
}

async function handleAdminTokens(request, env) {
  if (request.method !== "GET") return text("method not allowed", 405);
  if (!requireAdmin(request, env)) return text("unauthorized", 401);
  const tokens = await listTokens(env.DB);
  return json({ tokens });
}

async function handleAdminTokenCreate(request, env) {
  if (request.method !== "POST") return text("method not allowed", 405);
  if (!requireAdmin(request, env)) return text("unauthorized", 401);
  const body = await request.json().catch(() => ({}));
  const kind = body.kind || "subscribe";
  if (!["register", "subscribe"].includes(kind)) return text("kind must be register or subscribe", 400);
  const token = randomToken();
  const id = crypto.randomUUID();
  await insertToken(env.DB, { id, kind, token });
  const baseUrl = new URL(request.url).origin;
  const payload = {
    id,
    kind,
    token,
    token_hint: tokenHint(token)
  };
  if (kind === "subscribe") {
    payload.subscription_url = `${baseUrl}/sub?token=${token}`;
  }
  await writeAudit(env.DB, { event_type: "token_create", ...requestMeta(request), token_hint: tokenHint(token), status: 200 });
  return json(payload);
}

function nodeSummary(node) {
  return {
    node_id: node.node_id,
    name: node.name,
    region: node.region || "",
    host: node.host,
    ip: node.ip || "",
    protocols: (node.protocols || []).map((protocol) => ({ type: protocol.type, port: protocol.port })),
    updated_at: node.updated_at
  };
}

async function readLimitedJson(request, maxBytes) {
  const textBody = await request.text();
  if (new TextEncoder().encode(textBody).byteLength > maxBytes) {
    throw new Error("request body too large");
  }
  return JSON.parse(textBody);
}

function json(value, status = 200) {
  return new Response(JSON.stringify(value) + "\n", {
    status,
    headers: { "content-type": "application/json; charset=utf-8" }
  });
}

function text(value, status = 200) {
  return new Response(value + "\n", {
    status,
    headers: { "content-type": "text/plain; charset=utf-8" }
  });
}

export { hashToken };
