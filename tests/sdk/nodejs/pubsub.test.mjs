import { test } from "node:test";
import assert from "node:assert/strict";
import { doJSON, hasOwn, projectID, requireReady, requireToken, uniqueID } from "./helpers.mjs";

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
  assert.ok(hasOwn(parsed, "topics"), listed.body);
});
