import { test } from "node:test";
import assert from "node:assert/strict";
import { doJSON, hasOwn, projectID, requireReady, requireToken, uniqueID } from "./helpers.mjs";

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

test("list kms key rings smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/v1/projects/${project}/locations/global/keyRings`,
    token,
  );
  assert.equal(status, 200, `list KMS keyRings status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(hasOwn(parsed, "keyRings"), body);
});
