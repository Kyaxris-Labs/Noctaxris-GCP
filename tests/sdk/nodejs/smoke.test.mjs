import { test } from "node:test";
import assert from "node:assert/strict";

function endpoint() {
  return (process.env.NOCTAXRIS_GCP_ENDPOINT || "").trim().replace(/\/$/, "");
}

function projectID() {
  return (process.env.NOCTAXRIS_GCP_PROJECT || "").trim() || "noctaxris-gcp-local";
}

function uniqueID(prefix) {
  return `${prefix}-${Date.now()}${Math.floor(Math.random() * 1e6)}`;
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

function requireToken(t) {
  const token = (process.env.NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN || "").trim();
  if (!token) {
    t.skip("NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN unset; soft-skip authenticated smoke");
    return null;
  }
  return token;
}

async function doJSON(method, url, token, body) {
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

test("ready and get project smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON("GET", `${ep}/v3/projects/${project}`, token);
  assert.equal(status, 200, `get project status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.equal(parsed.projectId, project, body);
});

test("get organization smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;

  const { status, body } = await doJSON(
    "GET",
    `${ep}/v3/organizations/noctaxris-gcp-org`,
    token,
  );
  assert.equal(status, 200, `get organization status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.equal(parsed.name, "organizations/noctaxris-gcp-org", body);
});

test("list folders smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;

  const parent = encodeURIComponent("organizations/noctaxris-gcp-org");
  const { status, body } = await doJSON("GET", `${ep}/v3/folders?parent=${parent}`, token);
  assert.equal(status, 200, `list folders status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(Object.prototype.hasOwnProperty.call(parsed, "folders"), body);
});

test("list buckets smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/storage/v1/b?project=${encodeURIComponent(project)}`,
    token,
  );
  assert.equal(status, 200, `list buckets status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.equal(parsed.kind, "storage#buckets", body);
});

test("pubsub create and list topics smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();
  const topicID = uniqueID("sdk-topic");
  const topicPath = `${ep}/v1/projects/${project}/topics/${topicID}`;

  const created = await doJSON("PUT", topicPath, token, {});
  assert.equal(created.status, 200, `create topic status=${created.status} body=${created.body}`);
  t.after(async () => {
    try {
      await doJSON("DELETE", topicPath, token);
    } catch {
      /* best-effort cleanup */
    }
  });

  const listed = await doJSON("GET", `${ep}/v1/projects/${project}/topics`, token);
  assert.equal(listed.status, 200, `list topics status=${listed.status} body=${listed.body}`);
  const parsed = JSON.parse(listed.body);
  assert.ok(Object.prototype.hasOwnProperty.call(parsed, "topics"), listed.body);
});

test("secret manager create access smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();
  const secretID = uniqueID("sdk-secret");
  const base = `${ep}/v1/projects/${project}/secrets/${secretID}`;

  const created = await doJSON(
    "POST",
    `${ep}/v1/projects/${project}/secrets?secretId=${encodeURIComponent(secretID)}`,
    token,
    { replication: { automatic: {} } },
  );
  assert.equal(created.status, 200, `create secret status=${created.status} body=${created.body}`);
  t.after(async () => {
    try {
      await doJSON("DELETE", base, token);
    } catch {
      /* best-effort cleanup */
    }
  });

  const payload = Buffer.from("sdk-smoke", "utf8").toString("base64");
  const added = await doJSON("POST", `${base}:addVersion`, token, {
    payload: { data: payload },
  });
  assert.equal(added.status, 200, `addVersion status=${added.status} body=${added.body}`);

  const accessed = await doJSON("GET", `${base}/versions/latest:access`, token);
  assert.equal(accessed.status, 200, `access secret status=${accessed.status} body=${accessed.body}`);
  const parsed = JSON.parse(accessed.body);
  const got = Buffer.from(parsed.payload?.data || "", "base64").toString("utf8");
  assert.equal(got, "sdk-smoke", accessed.body);
});

test("list cloud run services smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/v2/projects/${project}/locations/us-central1/services`,
    token,
  );
  assert.equal(status, 200, `list run services status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(Object.prototype.hasOwnProperty.call(parsed, "services"), body);
});

test("list artifact registry repositories smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/v1/projects/${project}/locations/us-central1/repositories`,
    token,
  );
  assert.equal(status, 200, `list repositories status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(Object.prototype.hasOwnProperty.call(parsed, "repositories"), body);
});

test("list workflows smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/v1/projects/${project}/locations/us-central1/workflows`,
    token,
  );
  assert.equal(status, 200, `list workflows status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(Object.prototype.hasOwnProperty.call(parsed, "workflows"), body);
});

test("get app engine app smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON("GET", `${ep}/v1/apps/${project}`, token);
  if (status === 404) {
    t.skip("App Engine app not created; soft-skip get app smoke");
    return;
  }
  assert.equal(status, 200, `get app status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  if (parsed.id) {
    assert.equal(parsed.id, project, body);
  }
});
