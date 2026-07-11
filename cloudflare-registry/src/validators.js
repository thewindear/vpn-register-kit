const nodeIdPattern = /^[A-Za-z0-9][A-Za-z0-9._-]{1,63}$/;
const hostPattern = /^[A-Za-z0-9][A-Za-z0-9.-]{0,252}[A-Za-z0-9]$/;

export function validateNode(node) {
  if (!node || typeof node !== "object") throw new Error("node must be an object");
  if (node.schema_version !== 1) throw new Error("schema_version must be 1");
  if (!nodeIdPattern.test(node.node_id || "")) throw new Error("invalid node_id");
  if (!node.name || node.name.length > 80) throw new Error("invalid name");
  if (!isValidHost(node.host)) throw new Error("invalid host");
  if (!Array.isArray(node.protocols) || node.protocols.length === 0) {
    throw new Error("at least one protocol is required");
  }
  for (const protocol of node.protocols) {
    validateProtocol(protocol);
  }
}

function validateProtocol(protocol) {
  if (!Number.isInteger(protocol.port) || protocol.port < 1 || protocol.port > 65535) {
    throw new Error(`invalid port for ${protocol.type}`);
  }
  if (protocol.type === "trojan") {
    if (!protocol.password) throw new Error("trojan password is required");
    if (protocol.sni && !isValidHost(protocol.sni)) throw new Error("invalid trojan sni");
    return;
  }
  if (protocol.type === "shadowsocks") {
    if (!protocol.method || !protocol.password) {
      throw new Error("shadowsocks method and password are required");
    }
    return;
  }
  throw new Error(`unsupported protocol ${protocol.type}`);
}

function isValidHost(host) {
  if (!host || host.length > 253) return false;
  if (/^\d{1,3}(\.\d{1,3}){3}$/.test(host)) return true;
  return host.includes(".") && hostPattern.test(host);
}
