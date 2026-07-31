import { test } from "node:test";
import assert from "node:assert/strict";
import { doJSON, hasOwn, projectID, requireReady, requireToken } from "./helpers.mjs";

test("list logging sinks smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON("GET", `${ep}/v2/projects/${project}/sinks`, token);
  assert.equal(status, 200, `list Logging sinks status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(hasOwn(parsed, "sinks"), body);
});

test("list monitoring alert policies smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();

  const { status, body } = await doJSON(
    "GET",
    `${ep}/v3/projects/${project}/alertPolicies`,
    token,
  );
  assert.equal(status, 200, `list Monitoring alertPolicies status=${status} body=${body}`);
  const parsed = JSON.parse(body);
  assert.ok(hasOwn(parsed, "alertPolicies"), body);
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
  assert.ok(hasOwn(parsed, "jobs"), body);
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
  assert.ok(hasOwn(parsed, "candidates"), body);
});
