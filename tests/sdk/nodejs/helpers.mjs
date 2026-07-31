export function endpoint() {
  return (process.env.NOCTAXRIS_GCP_ENDPOINT || "").trim().replace(/\/$/, "");
}

export function projectID() {
  return (process.env.NOCTAXRIS_GCP_PROJECT || "").trim() || "noctaxris-gcp-local";
}

export function uniqueID(prefix) {
  return `${prefix}-${Date.now()}${Math.floor(Math.random() * 1e6)}`;
}

export function truthyEnv(name) {
  const v = (process.env[name] || "").trim();
  return v === "1" || v.toLowerCase() === "true";
}

export async function requireReady(t) {
  const ep = endpoint();
  if (!ep) {
    t.skip("NOCTAXRIS_GCP_ENDPOINT unset; soft-skip live smoke");
    return null;
  }
  try {
    const res = await fetch(`${ep}/_noctaxris-gcp/ready`, {
      signal: AbortSignal.timeout(2000),
    });
    if (!res.ok) {
      t.skip(`Noctaxris-GCP not ready at ${ep}: status ${res.status}`);
      return null;
    }
  } catch (err) {
    t.skip(`Noctaxris-GCP not reachable at ${ep}: ${err}`);
    return null;
  }
  return ep;
}

export function requireToken(t) {
  const token = (process.env.NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN || "").trim();
  if (!token) {
    t.skip("NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN unset; soft-skip authenticated smoke");
    return null;
  }
  return token;
}

export async function doJSON(method, url, token, body) {
  const opts = {
    method,
    headers: { Authorization: `Bearer ${token}` },
    signal: AbortSignal.timeout(5000),
  };
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(url, opts);
  const text = await res.text();
  return { status: res.status, body: text };
}

export function hasOwn(obj, key) {
  return Object.prototype.hasOwnProperty.call(obj, key);
}
