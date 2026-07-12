#!/usr/bin/env node
import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { randomBytes } from "node:crypto";
import { createInterface } from "node:readline/promises";
import { stdin as input, stdout as output } from "node:process";

const rl = createInterface({ input, output });

async function main() {
  try {
    assertProjectRoot();
    await ensurePnpmInstall();
    await ensureWranglerLogin();
    const database = await ensureD1Database();
    await ensureAdminToken();
    await runMigrations();
    await deployWorker();
    await maybeCreateInitialTokens();
    console.log("\nCloudflare registry deployment flow complete.");
  } catch (error) {
    console.error(`\nerror: ${error.message}`);
    process.exit(1);
  } finally {
    rl.close();
  }
}

function assertProjectRoot() {
  if (!existsSync("wrangler.toml") || !existsSync("src/index.js")) {
    throw new Error("run this script from cloudflare-registry/");
  }
}

async function ensurePnpmInstall() {
  requireCommand("pnpm");
  if (!existsSync("node_modules/.bin/wrangler")) {
    const yes = await confirm("node_modules is missing. Run pnpm install now?", true);
    if (!yes) throw new Error("pnpm install is required before deployment");
    run("pnpm", ["install"]);
  }
}

async function ensureWranglerLogin() {
  const result = runWranglerWhoami();
  if (result.ok) {
    if (result.output) console.log(result.output);
    return;
  }
  if (result.output) {
    console.log(result.output);
  }
  const yes = await confirm("Wrangler is not logged in. Run wrangler login now?", true);
  if (!yes) throw new Error("Cloudflare login is required");
  run("pnpm", ["exec", "wrangler", "login"]);
  const afterLogin = runWranglerWhoami();
  if (!afterLogin.ok) {
    if (afterLogin.output) console.log(afterLogin.output);
    throw new Error("Wrangler still is not authenticated after login");
  }
  if (afterLogin.output) console.log(afterLogin.output);
}

async function ensureD1Database() {
  const current = parseWranglerToml();
  const defaultName = current.databaseName || "vpn-register-kit";
  const currentID = current.databaseID;
  if (currentID && currentID !== "replace-with-d1-database-id") {
    console.log(`Using existing D1 database_id from wrangler.toml: ${currentID}`);
    return { name: defaultName, id: currentID };
  }

  const create = await confirm(`Create D1 database "${defaultName}" now?`, true);
  let databaseID = "";
  let databaseName = defaultName;
  if (create) {
    const result = runCapture("pnpm", ["exec", "wrangler", "d1", "create", defaultName]);
    console.log(result);
    databaseID = extractDatabaseID(result);
    const extractedName = extractDatabaseName(result);
    if (extractedName) databaseName = extractedName;
    if (!databaseID) {
      databaseID = await askRequired("Could not parse database_id. Paste database_id from wrangler output");
    }
  } else {
    databaseName = await askDefault("D1 database_name", defaultName);
    databaseID = await askRequired("D1 database_id");
  }
  updateWranglerToml(databaseName, databaseID);
  console.log("Updated wrangler.toml with D1 database binding.");
  return { name: databaseName, id: databaseID };
}

async function ensureAdminToken() {
  const generated = randomBytes(32).toString("hex");
  console.log("\nADMIN_TOKEN is the token used by local CLI/admin API.");
  console.log("A strong token has been generated for this setup:");
  console.log(`  ${generated}`);
  const token = await askDefault("ADMIN_TOKEN to store in Cloudflare Secret", generated);
  const yes = await confirm("Store ADMIN_TOKEN with wrangler secret put now?", true);
  if (!yes) {
    console.log("Skipping secret upload. Store it later with: pnpm exec wrangler secret put ADMIN_TOKEN");
    return;
  }
  const child = spawnSync("pnpm", ["exec", "wrangler", "secret", "put", "ADMIN_TOKEN"], {
    input: `${token}\n`,
    encoding: "utf8",
    stdio: ["pipe", "inherit", "inherit"]
  });
  if (child.status !== 0) {
    throw new Error("wrangler secret put ADMIN_TOKEN failed");
  }
  console.log("\nSave this ADMIN_TOKEN locally; Cloudflare will not show it again:");
  console.log(`  ${token}`);
}

async function runMigrations() {
  const yes = await confirm("Apply D1 migrations to remote database now?", true);
  if (!yes) return;
  run("pnpm", ["db:migrate:remote"]);
}

async function deployWorker() {
  const yes = await confirm("Deploy Worker now?", true);
  if (!yes) return;
  run("pnpm", ["exec", "wrangler", "deploy"]);
}

async function maybeCreateInitialTokens() {
  const yes = await confirm("Create initial register and subscribe tokens now?", true);
  if (!yes) return;
  const registryURL = await askRequired("Worker public URL, e.g. https://sub.example.com or https://vpn-register-kit.<account>.workers.dev");
  const adminToken = await askRequired("ADMIN_TOKEN");
  const env = {
    ...process.env,
    CF_REGISTRY_URL: registryURL,
    CF_ADMIN_TOKEN: adminToken
  };
  run("node", ["scripts/create-token.js", "--kind", "register"], { env });
  run("node", ["scripts/create-token.js", "--kind", "subscribe"], { env });
}

function parseWranglerToml() {
  const text = readFileSync("wrangler.toml", "utf8");
  return {
    text,
    databaseName: matchValue(text, "database_name"),
    databaseID: matchValue(text, "database_id")
  };
}

function matchValue(text, key) {
  const match = text.match(new RegExp(`^${key}\\s*=\\s*"([^"]*)"`, "m"));
  return match ? match[1] : "";
}

function updateWranglerToml(databaseName, databaseID) {
  const current = parseWranglerToml();
  let next = current.text;
  next = next.replace(/^database_name\s*=\s*"[^"]*"/m, `database_name = "${databaseName}"`);
  next = next.replace(/^database_id\s*=\s*"[^"]*"/m, `database_id = "${databaseID}"`);
  writeFileSync("wrangler.toml", next);
}

function extractDatabaseID(outputText) {
  const uuid = outputText.match(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i);
  return uuid ? uuid[0] : "";
}

function extractDatabaseName(outputText) {
  const match = outputText.match(/database_name\s*=\s*"([^"]+)"/);
  return match ? match[1] : "";
}

async function askRequired(question) {
  const answer = (await rl.question(`${question}: `)).trim();
  if (!answer) throw new Error(`${question} is required`);
  return answer;
}

async function askDefault(question, defaultValue) {
  const answer = (await rl.question(`${question} [${defaultValue}]: `)).trim();
  return answer || defaultValue;
}

async function confirm(question, defaultYes) {
  const suffix = defaultYes ? "Y/n" : "y/N";
  const answer = (await rl.question(`${question} [${suffix}]: `)).trim().toLowerCase();
  if (!answer) return defaultYes;
  return ["y", "yes"].includes(answer);
}

function requireCommand(command) {
  const result = spawnSync(command, ["--version"], { stdio: "ignore" });
  if (result.status !== 0) {
    throw new Error(`${command} is required`);
  }
}

function run(command, args, options = {}) {
  console.log(`\n$ ${command} ${args.join(" ")}`);
  const result = spawnSync(command, args, {
    stdio: "inherit",
    ...options
  });
  if (result.status !== 0) {
    throw new Error(`${command} ${args.join(" ")} failed`);
  }
}

function runCapture(command, args) {
  console.log(`\n$ ${command} ${args.join(" ")}`);
  try {
    return execFileSync(command, args, { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
  } catch (error) {
    const stderr = error.stderr?.toString() || "";
    const stdout = error.stdout?.toString() || "";
    throw new Error(`${command} ${args.join(" ")} failed\n${stdout}${stderr}`);
  }
}

function runWranglerWhoami() {
  const result = spawnSync("pnpm", ["exec", "wrangler", "whoami"], { encoding: "utf8" });
  const outputText = `${result.stdout || ""}${result.stderr || ""}`.trim();
  if (result.status === 0 && !isWranglerUnauthenticated(outputText)) {
    return { ok: true, output: outputText };
  }
  return { ok: false, output: outputText };
}

function isWranglerUnauthenticated(outputText) {
  return /not authenticated|wrangler login|you are not authenticated/i.test(outputText || "");
}

main();
