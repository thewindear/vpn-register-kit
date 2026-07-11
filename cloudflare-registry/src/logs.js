export function requestMeta(request) {
  return {
    remote_ip: request.headers.get("cf-connecting-ip") || "",
    user_agent: request.headers.get("user-agent") || ""
  };
}

export function logInfo(message, fields = {}) {
  console.log(`[INFO] ${message} ${formatFields(fields)}`.trim());
}

export function logWarn(message, fields = {}) {
  console.warn(`[WARN] ${message} ${formatFields(fields)}`.trim());
}

export function logError(message, fields = {}) {
  console.error(`[ERROR] ${message} ${formatFields(fields)}`.trim());
}

function formatFields(fields) {
  return Object.entries(fields)
    .filter(([, value]) => value !== undefined && value !== "")
    .map(([key, value]) => `${key}=${JSON.stringify(value)}`)
    .join(" ");
}
