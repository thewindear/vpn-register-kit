#!/usr/bin/env node
import { adminRequest, argValue } from "./admin-client.js";

const kind = argValue("--kind", "subscribe");
if (!["register", "subscribe"].includes(kind)) {
  console.error("error: --kind must be register or subscribe");
  process.exit(1);
}

try {
  const result = await adminRequest("/admin/tokens/create", {
    method: "POST",
    body: JSON.stringify({ kind })
  });
  console.log(`Created ${result.kind} token:`);
  console.log(`  ${result.token}`);
  if (result.subscription_url) {
    console.log("");
    console.log("Subscription URL:");
    console.log(`  ${result.subscription_url}`);
  }
} catch (error) {
  console.error(`error: ${error.message}`);
  process.exit(1);
}
