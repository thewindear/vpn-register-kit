export function adminConfig() {
  const baseUrl = process.env.CF_REGISTRY_URL;
  const adminToken = process.env.CF_ADMIN_TOKEN;
  if (!baseUrl) {
    throw new Error("CF_REGISTRY_URL is required, for example https://sub.example.com");
  }
  if (!adminToken) {
    throw new Error("CF_ADMIN_TOKEN is required");
  }
  return {
    baseUrl: baseUrl.replace(/\/+$/, ""),
    adminToken
  };
}

export async function adminRequest(path, options = {}) {
  const cfg = adminConfig();
  const response = await fetch(`${cfg.baseUrl}${path}`, {
    ...options,
    headers: {
      Authorization: `Bearer ${cfg.adminToken}`,
      "Content-Type": "application/json",
      ...(options.headers || {})
    }
  });
  const text = await response.text();
  let body;
  try {
    body = text ? JSON.parse(text) : null;
  } catch {
    body = text;
  }
  if (!response.ok) {
    throw new Error(`request failed status=${response.status} body=${text}`);
  }
  return body;
}

export function argValue(name, fallback = "") {
  const index = process.argv.indexOf(name);
  if (index === -1) return fallback;
  return process.argv[index + 1] || fallback;
}
