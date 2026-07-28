import { test } from "node:test";
import assert from "node:assert/strict";

function endpoint() {
  return (process.env.NOCTAXRIS_GCP_ENDPOINT || "").trim().replace(/\/$/, "");
}

async function requireReady(t) {
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

test("ready and get project smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;

  const token = (process.env.NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN || "").trim();
  if (!token) {
    t.skip("NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN unset; soft-skip authenticated smoke");
    return;
  }
  const project =
    (process.env.NOCTAXRIS_GCP_PROJECT || "").trim() || "noctaxris-gcp-local";

  const res = await fetch(`${ep}/v3/projects/${project}`, {
    headers: { Authorization: `Bearer ${token}` },
    signal: AbortSignal.timeout(5000),
  });
  const body = await res.text();
  assert.equal(res.status, 200, `get project status=${res.status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.equal(parsed.projectId, project, body);
});
