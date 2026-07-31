import { test } from "node:test";
import assert from "node:assert/strict";
import { doJSON, hasOwn, projectID, requireReady, requireToken, uniqueID } from "./helpers.mjs";

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
  assert.ok(hasOwn(parsed, "folders"), body);
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

test("list service usage services smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON("GET", `${ep}/v1/projects/${project}/services`, token);
  assert.equal(status, 200, `list Service Usage services status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(hasOwn(parsed, "services"), body);
});
