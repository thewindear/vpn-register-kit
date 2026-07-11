#!/usr/bin/env node
import { adminRequest } from "./admin-client.js";

try {
  const result = await adminRequest("/admin/nodes");
  console.log("Registered nodes:");
  for (const node of result.nodes || []) {
    const protocols = (node.protocols || []).map((item) => `${item.type}:${item.port}`).join(",");
    console.log(`- ${node.node_id.padEnd(16)} ${node.name.padEnd(16)} ${node.region.padEnd(6)} ${node.host.padEnd(32)} ${protocols} updated=${node.updated_at || "-"}`);
  }
} catch (error) {
  console.error(`error: ${error.message}`);
  process.exit(1);
}
