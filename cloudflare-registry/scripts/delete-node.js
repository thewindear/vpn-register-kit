#!/usr/bin/env node
import { adminRequest, argValue } from "./admin-client.js";

const nodeId = argValue("--id");
if (!nodeId) {
  console.error("error: --id is required");
  process.exit(1);
}

try {
  await adminRequest(`/admin/nodes/${encodeURIComponent(nodeId)}`, { method: "DELETE" });
  console.log(`Deleted node: ${nodeId}`);
} catch (error) {
  console.error(`error: ${error.message}`);
  process.exit(1);
}
