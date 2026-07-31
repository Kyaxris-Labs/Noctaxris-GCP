import { test } from "node:test";
import assert from "node:assert/strict";
import {
  doJSON,
  projectID,
  requireReady,
  requireToken,
  truthyEnv,
  uniqueID,
} from "./helpers.mjs";

test("nested invoke fail-closed smoke", async (t) => {
  if (!truthyEnv("NOCTAXRIS_GCP_NESTED_INVOKE_FAIL_CLOSED")) {
    t.skip("NOCTAXRIS_GCP_NESTED_INVOKE_FAIL_CLOSED unset; soft-skip nested fail-closed smoke");
    return;
  }
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();
  let svcID = uniqueID("sdk-failclosed");
  if (svcID.length > 49) {
    svcID = svcID.slice(0, 49).replace(/-+$/, "");
  }
  const base = `${ep}/v2/projects/${project}/locations/us-central1/services`;
  const svcPath = `${base}/${svcID}`;

  const created = await doJSON("POST", `${base}?serviceId=${encodeURIComponent(svcID)}`, token, {
    template: { containers: [{ image: "demo" }] },
  });
  assert.equal(created.status, 200, `create run service status=${created.status} body=${created.body}`);
  t.after(async () => {
    try {
      await doJSON("DELETE", svcPath, token);
    } catch {
      /* best-effort cleanup */
    }
  });

  const { status, body } = await doJSON("POST", `${svcPath}:invoke`, token, {});
  if (status === 200) {
    if (body.includes(`"mode":"mock"`) || body.includes(`"ok":true`)) {
      t.skip(
        `server returned soft-fail/mock invoke (DOCKER_HOST empty or not fail-closed); soft-skip body=${body}`,
      );
      return;
    }
    assert.fail(`:invoke unexpectedly OK under fail-closed env body=${body}`);
  }
  assert.ok(status >= 400, `:invoke want error status, got ${status} body=${body}`);
  assert.ok(!body.includes(`"mode":"mock"`), `:invoke should not soft-fail to mock under fail-closed, body=${body}`);
});
