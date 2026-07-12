import assert from "node:assert/strict";
import { webcrypto } from "node:crypto";
import test from "node:test";
import worker from "../src/index.js";
import { hashToken, tokenHint } from "../src/auth.js";
import { MemoryD1 } from "./memory-d1.js";

if (!globalThis.crypto) {
  Object.defineProperty(globalThis, "crypto", { value: webcrypto });
}

async function makeEnv() {
  const db = new MemoryD1();
  const registerToken = "reg-test-token";
  const subscribeToken = "sub-test-token";
  await db.prepare(
    "INSERT INTO tokens (id, kind, token_hash, token_hint, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?)"
  ).bind("reg-1", "register", await hashToken(registerToken), tokenHint(registerToken), 1, "2026-07-12T00:00:00.000Z").run();
  await db.prepare(
    "INSERT INTO tokens (id, kind, token_hash, token_hint, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?)"
  ).bind("sub-1", "subscribe", await hashToken(subscribeToken), tokenHint(subscribeToken), 1, "2026-07-12T00:00:00.000Z").run();
  return {
    env: { DB: db, ADMIN_TOKEN: "admin-test-token" },
    registerToken,
    subscribeToken
  };
}

function nodePayload() {
  return {
    schema_version: 1,
    node_id: "hk01",
    name: "HK-01",
    region: "HK",
    host: "hk01.example.com",
    ip: "203.0.113.10",
    protocols: [
      { type: "trojan", port: 443, password: "trojan-pass", sni: "hk01.example.com", tls: true },
      { type: "shadowsocks", port: 8080, method: "aes-128-gcm", password: "ss-pass" }
    ]
  };
}

async function registerNode(env, registerToken) {
  return worker.fetch(new Request("https://sub.example.com/register", {
    method: "POST",
    headers: {
      Authorization: `Bearer ${registerToken}`,
      "Content-Type": "application/json"
    },
    body: JSON.stringify(nodePayload())
  }), env);
}

test("register accepts existing node installer payload and default subscription returns links", async () => {
  const { env, registerToken, subscribeToken } = await makeEnv();
  const registerRes = await registerNode(env, registerToken);
  assert.equal(registerRes.status, 200);

  const subRes = await worker.fetch(new Request(`https://sub.example.com/sub?token=${subscribeToken}`), env);
  assert.equal(subRes.status, 200);
  assert.equal(subRes.headers.get("content-type"), "text/plain; charset=utf-8");
  const body = await subRes.text();
  assert.match(body, /trojan:\/\/trojan-pass@hk01\.example\.com:443/);
  assert.match(body, /ss:\/\//);
  assert.match(body, /#HK-01-SS/);
});

test("subscription supports clash and sing-box formats", async () => {
  const { env, registerToken, subscribeToken } = await makeEnv();
  await registerNode(env, registerToken);

  const clashRes = await worker.fetch(new Request(`https://sub.example.com/sub?token=${subscribeToken}&format=clash`), env);
  assert.equal(clashRes.status, 200);
  assert.equal(clashRes.headers.get("content-type"), "text/yaml; charset=utf-8");
  const clash = await clashRes.text();
  assert.match(clash, /proxies:/);
  assert.match(clash, /proxy-groups:/);
  assert.match(clash, /rule-providers:/);
  assert.match(clash, /cdn\.jsdelivr\.net\/gh\/Loyalsoldier\/clash-rules@release\/direct\.txt/);
  assert.match(clash, /RULE-SET,lancidr,DIRECT/);
  assert.match(clash, /RULE-SET,direct,DIRECT/);
  assert.match(clash, /RULE-SET,cncidr,DIRECT/);
  assert.match(clash, /RULE-SET,proxy,PROXY/);
  assert.match(clash, /rules:/);
  assert.match(clash, /MATCH,PROXY/);

  const singBoxRes = await worker.fetch(new Request(`https://sub.example.com/sub?token=${subscribeToken}&format=sing-box`), env);
  assert.equal(singBoxRes.status, 200);
  assert.equal(singBoxRes.headers.get("content-type"), "application/json; charset=utf-8");
  const singBox = await singBoxRes.json();
  assert.ok(Array.isArray(singBox.outbounds));
});

test("subscription supports shadowrocket config format with rules", async () => {
  const { env, registerToken, subscribeToken } = await makeEnv();
  await registerNode(env, registerToken);

  const res = await worker.fetch(new Request(`https://sub.example.com/sub?token=${subscribeToken}&format=shadowrocket-conf`), env);
  assert.equal(res.status, 200);
  assert.equal(res.headers.get("content-type"), "text/plain; charset=utf-8");
  const conf = await res.text();
  assert.match(conf, /\[Proxy\]/);
  assert.match(conf, /HK-01-Trojan = trojan, hk01\.example\.com, 443, password=trojan-pass, over-tls=true, tls-verification=true, sni=hk01\.example\.com/);
  assert.match(conf, /HK-01-SS = ss, hk01\.example\.com, 8080, encrypt-method=aes-128-gcm, password=ss-pass/);
  assert.match(conf, /\[Proxy Group\]/);
  assert.match(conf, /PROXY = select, HK-01-Trojan, HK-01-SS, DIRECT/);
  assert.match(conf, /\[Rule\]/);
  assert.match(conf, /DOMAIN-SUFFIX,baidu\.com,DIRECT/);
  assert.match(conf, /GEOIP,CN,DIRECT/);
  assert.match(conf, /FINAL,PROXY/);
});

test("invalid subscribe token is rejected", async () => {
  const { env } = await makeEnv();
  const res = await worker.fetch(new Request("https://sub.example.com/sub?token=bad-token"), env);
  assert.equal(res.status, 401);
});

test("admin can create subscribe token and list tokens", async () => {
  const { env } = await makeEnv();
  const createRes = await worker.fetch(new Request("https://sub.example.com/admin/tokens/create", {
    method: "POST",
    headers: {
      Authorization: "Bearer admin-test-token",
      "Content-Type": "application/json"
    },
    body: JSON.stringify({ kind: "subscribe" })
  }), env);
  assert.equal(createRes.status, 200);
  const created = await createRes.json();
  assert.equal(created.kind, "subscribe");
  assert.ok(created.token);
  assert.ok(created.subscription_url.endsWith(`/sub?token=${created.token}`));

  const listRes = await worker.fetch(new Request("https://sub.example.com/admin/tokens", {
    headers: { Authorization: "Bearer admin-test-token" }
  }), env);
  assert.equal(listRes.status, 200);
  const list = await listRes.json();
  assert.equal(list.tokens.length, 3);
  assert.equal(list.tokens.some((item) => item.token === created.token), false);
});

test("api nodes returns summaries without protocol passwords", async () => {
  const { env, registerToken } = await makeEnv();
  await registerNode(env, registerToken);

  const res = await worker.fetch(new Request("https://sub.example.com/api/nodes", {
    headers: { Authorization: `Bearer ${registerToken}` }
  }), env);
  assert.equal(res.status, 200);
  const body = await res.json();
  assert.equal(body.nodes[0].node_id, "hk01");
  assert.deepEqual(body.nodes[0].protocols, [
    { type: "trojan", port: 443 },
    { type: "shadowsocks", port: 8080 }
  ]);
  assert.equal(JSON.stringify(body).includes("trojan-pass"), false);
});
