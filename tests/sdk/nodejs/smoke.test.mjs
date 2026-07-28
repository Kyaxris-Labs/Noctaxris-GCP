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

test("list compute instances smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/compute/v1/projects/${project}/zones/us-central1-a/instances`,
    token,
  );
  assert.equal(status, 200, `list compute instances status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.equal(parsed.kind, "compute#instanceList", body);
  assert.ok(Object.prototype.hasOwnProperty.call(parsed, "items"), body);
});

test("list dns managed zones smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/dns/v1/projects/${project}/managedZones`,
    token,
  );
  assert.equal(status, 200, `list managed zones status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(Object.prototype.hasOwnProperty.call(parsed, "managedZones"), body);
});

test("list bigtable instances smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/v2/projects/${project}/instances`,
    token,
  );
  assert.equal(status, 200, `list bigtable instances status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(Object.prototype.hasOwnProperty.call(parsed, "instances"), body);
});

test("list memorystore instances smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/v1/projects/${project}/locations/us-central1/instances`,
    token,
  );
  assert.equal(status, 200, `list memorystore instances status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(Object.prototype.hasOwnProperty.call(parsed, "instances"), body);
});

test("list dataflow jobs smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/v1b3/projects/${project}/locations/us-central1/jobs`,
    token,
  );
  assert.equal(status, 200, `list dataflow jobs status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(Object.prototype.hasOwnProperty.call(parsed, "jobs"), body);
});

test("list cloud armor security policies smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/compute/v1/projects/${project}/global/securityPolicies`,
    token,
  );
  assert.equal(status, 200, `list securityPolicies status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.equal(parsed.kind, "compute#securityPolicyList", body);
  assert.ok(Object.prototype.hasOwnProperty.call(parsed, "items"), body);
});

test("list certificate manager certificates smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/v1/projects/${project}/locations/global/certificates`,
    token,
  );
  assert.equal(status, 200, `list certificates status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(Object.prototype.hasOwnProperty.call(parsed, "certificates"), body);
});

test("list filestore instances smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/file/v1/projects/${project}/locations/us-central1/instances`,
    token,
  );
  assert.equal(status, 200, `list filestore instances status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(Object.prototype.hasOwnProperty.call(parsed, "instances"), body);
});

test("vertex ai generateContent smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "POST",
    `${ep}/v1/projects/${project}/locations/us-central1/publishers/google/models/gemini-1.5-flash:generateContent`,
    token,
    {
      contents: [{ role: "user", parts: [{ text: "sdk-smoke" }] }],
    },
  );
  assert.equal(status, 200, `generateContent status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(Object.prototype.hasOwnProperty.call(parsed, "candidates"), body);
});

test("iam generateAccessToken smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();
  let accountId = uniqueID("sdksa");
  if (accountId.length > 30) {
    accountId = accountId.slice(0, 30).replace(/-+$/, "");
  }
  const email = `${accountId}@${project}.iam.gserviceaccount.com`;
  const saPath = `${ep}/v1/projects/${project}/serviceAccounts/${email}`;

  const created = await doJSON("POST", `${ep}/v1/projects/${project}/serviceAccounts`, token, {
    accountId,
    serviceAccount: { displayName: "sdk smoke" },
  });
  assert.equal(created.status, 200, `create service account status=${created.status} body=${created.body}`);
  t.after(async () => {
    try {
      await doJSON("DELETE", saPath, token);
    } catch {
      /* best-effort cleanup */
    }
  });

  const { status, body } = await doJSON("POST", `${saPath}:generateAccessToken`, token, {
    scope: ["https://www.googleapis.com/auth/cloud-platform"],
    lifetime: "3600s",
  });
  assert.equal(status, 200, `generateAccessToken status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(parsed.accessToken, body);
});

test("gcs generateSignedUrl smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();
  const bucket = uniqueID("sdk-signed");
  const bucketPath = `${ep}/storage/v1/b/${encodeURIComponent(bucket)}`;

  const created = await doJSON(
    "POST",
    `${ep}/storage/v1/b?project=${encodeURIComponent(project)}`,
    token,
    { name: bucket },
  );
  assert.equal(created.status, 200, `create bucket status=${created.status} body=${created.body}`);
  t.after(async () => {
    try {
      await doJSON("DELETE", bucketPath, token);
    } catch {
      /* best-effort cleanup */
    }
  });

  const { status, body } = await doJSON(
    "POST",
    `${bucketPath}/o/smoke.txt:generateSignedUrl`,
    token,
    { method: "GET", expires: 600, alt: "media" },
  );
  assert.equal(status, 200, `generateSignedUrl status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(parsed.signedUrl, body);
});

test("sts token exchange smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();
  let poolId = uniqueID("sdk-sts-pool");
  if (poolId.length > 32) {
    poolId = poolId.slice(0, 32).replace(/-+$/, "");
  }
  const providerId = "oidc";
  const poolBase = `${ep}/v1/projects/${project}/locations/global/workloadIdentityPools/${poolId}`;
  const providerName = `projects/${project}/locations/global/workloadIdentityPools/${poolId}/providers/${providerId}`;

  const pool = await doJSON(
    "POST",
    `${ep}/v1/projects/${project}/locations/global/workloadIdentityPools?workloadIdentityPoolId=${encodeURIComponent(poolId)}`,
    token,
    { displayName: "sdk sts pool" },
  );
  assert.equal(pool.status, 200, `create WIF pool status=${pool.status} body=${pool.body}`);
  t.after(async () => {
    try {
      await doJSON("DELETE", poolBase, token);
    } catch {
      /* best-effort cleanup */
    }
  });

  const provider = await doJSON(
    "POST",
    `${poolBase}/providers?workloadIdentityPoolProviderId=${encodeURIComponent(providerId)}`,
    token,
    { displayName: "sdk oidc", oidc: { issuerUri: "https://example.com" } },
  );
  assert.equal(provider.status, 200, `create WIF provider status=${provider.status} body=${provider.body}`);
  t.after(async () => {
    try {
      await doJSON("DELETE", `${poolBase}/providers/${providerId}`, token);
    } catch {
      /* best-effort cleanup */
    }
  });

  const form = new URLSearchParams({
    grant_type: "urn:ietf:params:oauth:grant-type:token-exchange",
    audience: `//iam.googleapis.com/${providerName}`,
    subject_token: "sdk-sts-sub",
    subject_token_type: "urn:ietf:params:oauth:token-type:jwt",
  });
  const res = await fetch(`${ep}/v1/token`, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: form.toString(),
    signal: AbortSignal.timeout(5000),
  });
  const body = await res.text();
  assert.equal(res.status, 200, `STS /v1/token status=${res.status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(parsed.access_token, body);
  assert.equal(parsed.token_type, "Bearer", body);
});

test("gcs retention delete deny smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();
  const bucket = uniqueID("sdk-retain");
  const bucketPath = `${ep}/storage/v1/b/${encodeURIComponent(bucket)}`;
  const objectPath = `${bucketPath}/o/${encodeURIComponent("held.txt")}`;

  const created = await doJSON(
    "POST",
    `${ep}/storage/v1/b?project=${encodeURIComponent(project)}`,
    token,
    { name: bucket },
  );
  assert.equal(created.status, 200, `create bucket status=${created.status} body=${created.body}`);
  t.after(async () => {
    try {
      await doJSON("DELETE", objectPath, token);
    } catch {
      /* best-effort cleanup */
    }
    try {
      await doJSON("DELETE", bucketPath, token);
    } catch {
      /* best-effort cleanup */
    }
  });

  const patched = await doJSON("PATCH", bucketPath, token, {
    retentionPolicy: { retentionPeriod: "3600" },
  });
  assert.equal(patched.status, 200, `patch retention status=${patched.status} body=${patched.body}`);

  const upRes = await fetch(
    `${ep}/upload/storage/v1/b/${encodeURIComponent(bucket)}/o?uploadType=media&name=${encodeURIComponent("held.txt")}`,
    {
      method: "POST",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "text/plain",
      },
      body: "held",
      signal: AbortSignal.timeout(5000),
    },
  );
  const upBody = await upRes.text();
  assert.equal(upRes.status, 200, `upload status=${upRes.status} body=${upBody}`);

  const deleted = await doJSON("DELETE", objectPath, token);
  assert.notEqual(deleted.status, 200, `delete under retention should fail; body=${deleted.body}`);
  const errBody = JSON.parse(deleted.body);
  assert.equal(errBody.error?.status, "FAILED_PRECONDITION", deleted.body);
});
