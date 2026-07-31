import { test } from "node:test";
import assert from "node:assert/strict";
import { doJSON, hasOwn, projectID, requireReady, requireToken } from "./helpers.mjs";

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
  assert.ok(hasOwn(parsed, "items"), body);
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
  assert.ok(hasOwn(parsed, "services"), body);
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
  assert.ok(hasOwn(parsed, "repositories"), body);
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
  assert.ok(hasOwn(parsed, "workflows"), body);
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

test("list cloud build builds smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON("GET", `${ep}/v1/projects/${project}/builds`, token);
  assert.equal(status, 200, `list Cloud Build builds status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(hasOwn(parsed, "builds"), body);
});

test("list cloud functions smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/v2/projects/${project}/locations/us-central1/functions`,
    token,
  );
  assert.equal(status, 200, `list Cloud Functions status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(hasOwn(parsed, "functions"), body);
});

test("list cloud scheduler jobs smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/v1/projects/${project}/locations/us-central1/jobs`,
    token,
  );
  assert.equal(status, 200, `list Scheduler jobs status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(hasOwn(parsed, "jobs"), body);
});

test("list cloud tasks queues smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/v2/projects/${project}/locations/us-central1/queues`,
    token,
  );
  assert.equal(status, 200, `list Cloud Tasks queues status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(hasOwn(parsed, "queues"), body);
});

test("list eventarc channels smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/v1/projects/${project}/locations/us-central1/channels`,
    token,
  );
  assert.equal(status, 200, `list Eventarc channels status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(hasOwn(parsed, "channels"), body);
});
