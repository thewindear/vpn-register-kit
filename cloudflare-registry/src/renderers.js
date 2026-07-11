export function renderSubscription(format, nodes) {
  const normalized = (format || "shadowrocket").toLowerCase();
  if (["shadowrocket", "links", "plain"].includes(normalized)) {
    return {
      body: renderLinks(nodes),
      contentType: "text/plain; charset=utf-8"
    };
  }
  if (normalized === "base64") {
    return {
      body: base64Encode(renderLinks(nodes)),
      contentType: "text/plain; charset=utf-8"
    };
  }
  if (["clash", "mihomo"].includes(normalized)) {
    return {
      body: renderClash(nodes),
      contentType: "text/yaml; charset=utf-8"
    };
  }
  if (["sing-box", "singbox"].includes(normalized)) {
    return {
      body: JSON.stringify(renderSingBox(nodes), null, 2) + "\n",
      contentType: "application/json; charset=utf-8"
    };
  }
  throw new Error(`unsupported format ${format}`);
}

export function renderLinks(nodes) {
  const links = [];
  for (const node of nodes) {
    for (const protocol of node.protocols || []) {
      if (protocol.type === "trojan") {
        const params = new URLSearchParams();
        if (protocol.sni) {
          params.set("peer", protocol.sni);
          params.set("sni", protocol.sni);
        }
        if (protocol.tls) params.set("security", "tls");
        const query = params.toString();
        links.push(
          `trojan://${encodeURIComponent(protocol.password)}@${node.host}:${protocol.port}${query ? `?${query}` : ""}#${encodeURIComponent(`${node.name}-Trojan`)}`
        );
      }
      if (protocol.type === "shadowsocks") {
        const userInfo = base64UrlEncode(`${protocol.method}:${protocol.password}`);
        links.push(`ss://${userInfo}@${node.host}:${protocol.port}#${encodeURIComponent(`${node.name}-SS`)}`);
      }
    }
  }
  return links.join("\n") + "\n";
}

export function renderClash(nodes) {
  const proxyNames = [];
  const lines = [
    "mixed-port: 7890",
    "allow-lan: false",
    "mode: rule",
    "log-level: info",
    "proxies:"
  ];
  for (const node of nodes) {
    for (const protocol of node.protocols || []) {
      if (protocol.type === "trojan") {
        const name = `${node.name}-Trojan`;
        proxyNames.push(name);
        lines.push(`  - name: ${yamlQuote(name)}`);
        lines.push("    type: trojan");
        lines.push(`    server: ${yamlQuote(node.host)}`);
        lines.push(`    port: ${protocol.port}`);
        lines.push(`    password: ${yamlQuote(protocol.password)}`);
        lines.push(`    sni: ${yamlQuote(protocol.sni || node.host)}`);
        lines.push("    skip-cert-verify: false");
        lines.push("    udp: true");
      }
      if (protocol.type === "shadowsocks") {
        const name = `${node.name}-SS`;
        proxyNames.push(name);
        lines.push(`  - name: ${yamlQuote(name)}`);
        lines.push("    type: ss");
        lines.push(`    server: ${yamlQuote(node.host)}`);
        lines.push(`    port: ${protocol.port}`);
        lines.push(`    cipher: ${yamlQuote(protocol.method)}`);
        lines.push(`    password: ${yamlQuote(protocol.password)}`);
        lines.push("    udp: true");
      }
    }
  }
  lines.push("proxy-groups:");
  lines.push("  - name: PROXY");
  lines.push("    type: select");
  lines.push("    proxies:");
  for (const name of proxyNames) lines.push(`      - ${yamlQuote(name)}`);
  lines.push("      - DIRECT");
  lines.push("rules:");
  lines.push("  - GEOIP,CN,DIRECT");
  lines.push("  - MATCH,PROXY");
  return lines.join("\n") + "\n";
}

export function renderSingBox(nodes) {
  const outbounds = [{ type: "direct", tag: "direct" }];
  for (const node of nodes) {
    for (const protocol of node.protocols || []) {
      if (protocol.type === "trojan") {
        outbounds.push({
          type: "trojan",
          tag: `${node.name}-Trojan`,
          server: node.host,
          server_port: protocol.port,
          password: protocol.password,
          tls: {
            enabled: Boolean(protocol.tls),
            server_name: protocol.sni || node.host
          }
        });
      }
      if (protocol.type === "shadowsocks") {
        outbounds.push({
          type: "shadowsocks",
          tag: `${node.name}-SS`,
          server: node.host,
          server_port: protocol.port,
          method: protocol.method,
          password: protocol.password
        });
      }
    }
  }
  return {
    log: { level: "info" },
    dns: { servers: [{ tag: "google", address: "8.8.8.8" }] },
    outbounds,
    route: { final: "direct" }
  };
}

function yamlQuote(value) {
  return `"${String(value).replaceAll("\\", "\\\\").replaceAll("\"", "\\\"")}"`;
}

function base64Encode(value) {
  if (typeof btoa === "function") return btoa(value);
  return Buffer.from(value, "utf8").toString("base64");
}

function base64UrlEncode(value) {
  return base64Encode(value).replaceAll("+", "-").replaceAll("/", "_").replaceAll("=", "");
}
