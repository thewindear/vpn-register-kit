#!/usr/bin/env node
import { adminRequest } from "./admin-client.js";

try {
  const result = await adminRequest("/admin/tokens");
  console.log("Tokens:");
  for (const token of result.tokens || []) {
    console.log(`- ${token.kind.padEnd(9)} ${token.token_hint} enabled=${token.enabled} created=${token.created_at} last_used=${token.last_used_at || "-"}`);
  }
} catch (error) {
  console.error(`error: ${error.message}`);
  process.exit(1);
}
