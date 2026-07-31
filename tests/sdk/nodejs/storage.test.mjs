import { test } from "node:test";
import assert from "node:assert/strict";
import { doJSON, hasOwn, projectID, requireReady, requireToken, uniqueID } from "./helpers.mjs";

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

test("list bigtable instances smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON("GET", `${ep}/v2/projects/${project}/instances`, token);
  assert.equal(status, 200, `list bigtable instances status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(hasOwn(parsed, "instances"), body);
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
  assert.ok(hasOwn(parsed, "instances"), body);
});

test("list cloud sql instances smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/sql/v1/projects/${project}/instances`,
    token,
  );
  assert.equal(status, 200, `list cloudsql instances status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(hasOwn(parsed, "items"), body);
});

test("list managed kafka clusters smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/v1/projects/${project}/locations/us-central1/clusters`,
    token,
  );
  assert.equal(status, 200, `list managed kafka clusters status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(hasOwn(parsed, "clusters"), body);
});

test("list bigquery datasets smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/bigquery/v2/projects/${project}/datasets`,
    token,
  );
  assert.equal(status, 200, `list BigQuery datasets status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(hasOwn(parsed, "datasets"), body);
});

test("list spanner instances smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON("GET", `${ep}/v1/projects/${project}/instances`, token);
  assert.equal(status, 200, `list Spanner instances status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(hasOwn(parsed, "instances"), body);
});
