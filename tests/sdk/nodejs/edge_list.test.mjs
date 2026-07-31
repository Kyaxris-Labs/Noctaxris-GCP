import { test } from "node:test";
import assert from "node:assert/strict";
import { doJSON, hasOwn, projectID, requireReady, requireToken } from "./helpers.mjs";

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
  assert.ok(hasOwn(parsed, "managedZones"), body);
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
  assert.ok(hasOwn(parsed, "items"), body);
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
  assert.ok(hasOwn(parsed, "certificates"), body);
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
  assert.ok(hasOwn(parsed, "instances"), body);
});

test("list gke clusters smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/container/v1/projects/${project}/locations/us-central1/clusters`,
    token,
  );
  assert.equal(status, 200, `list GKE clusters status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(hasOwn(parsed, "clusters"), body);
});

test("list load balancing backend services smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/compute/v1/projects/${project}/global/backendServices`,
    token,
  );
  assert.equal(status, 200, `list backendServices status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.equal(parsed.kind, "compute#backendServiceList", body);
  assert.ok(hasOwn(parsed, "items"), body);
});

test("list cdn distributions smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/v1/projects/${project}/global/distributions`,
    token,
  );
  assert.equal(status, 200, `list CDN distributions status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(hasOwn(parsed, "distributions"), body);
});
