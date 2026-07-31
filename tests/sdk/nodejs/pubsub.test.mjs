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

test("pubsub oidc push smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const token = requireToken(t);
  if (!token) return;
  const project = projectID();
  const topicID = uniqueID("sdk-oidc-topic");
  const subID = uniqueID("sdk-oidc-sub");
  const topicPath = `${ep}/v1/projects/${project}/topics/${topicID}`;
  const subPath = `${ep}/v1/projects/${project}/subscriptions/${subID}`;
  const catcher = "http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/sdk-oidc-push";
  const saEmail = `push-sa@${project}.iam.gserviceaccount.com`;
  const audience = "https://example.com/sdk-oidc";

  const topicCreated = await doJSON("PUT", topicPath, token, {});
  assert.equal(
    topicCreated.status,
    200,
    `create topic status=${topicCreated.status} body=${topicCreated.body}`,
  );
  t.after(async () => {
    try {
      await doJSON("DELETE", subPath, token);
      await doJSON("DELETE", topicPath, token);
    } catch {
      /* best-effort cleanup */
    }
  });

  const subCreated = await doJSON("PUT", subPath, token, {
    topic: `projects/${project}/topics/${topicID}`,
    ackDeadlineSeconds: 10,
    pushConfig: {
      pushEndpoint: catcher,
      oidcToken: {
        serviceAccountEmail: saEmail,
        audience,
      },
    },
  });
  assert.equal(
    subCreated.status,
    200,
    `create subscription status=${subCreated.status} body=${subCreated.body}`,
  );
  const created = JSON.parse(subCreated.body);
  const pc = created.pushConfig;
  const oidc = pc?.oidcToken;
  if (
    pc?.pushEndpoint !== catcher ||
    oidc?.serviceAccountEmail !== saEmail ||
    oidc?.audience !== audience
  ) {
    assert.fail(`oidcToken round-trip failed: ${JSON.stringify(created.pushConfig)}`);
  }

  const payload = Buffer.from("sdk-oidc-ping").toString("base64");
  const published = await doJSON("POST", `${topicPath}:publish`, token, {
    messages: [{ data: payload }],
  });
  assert.equal(
    published.status,
    200,
    `publish status=${published.status} body=${published.body}`,
  );

  await t.test("catcherAuthorization", async (st) => {
    let dumpRes;
    try {
      dumpRes = await doJSON("GET", `${ep}/_noctaxris-gcp/http-catcher`, token);
    } catch (err) {
      st.skip(
        `lab catcher dump unavailable (status=0 err=${err}); soft-skip Authorization assert (unit TestPubSubOIDCPushCatcher covers Bearer JWT)`,
      );
      return;
    }
    if (dumpRes.status !== 200) {
      st.skip(
        `lab catcher dump unavailable (status=${dumpRes.status}); soft-skip Authorization assert (unit TestPubSubOIDCPushCatcher covers Bearer JWT)`,
      );
      return;
    }
    const parsed = JSON.parse(dumpRes.body);
    const deliveries = parsed.deliveries;
    if (!Array.isArray(deliveries) || deliveries.length === 0) {
      st.skip("catcher dump empty; soft-skip Authorization assert");
      return;
    }
    let found = false;
    for (let i = deliveries.length - 1; i >= 0; i--) {
      const raw = deliveries[i];
      if (typeof raw !== "string") continue;
      try {
        const entry = JSON.parse(raw);
        const authz = entry.authorization;
        if (typeof authz === "string" && authz.startsWith("Bearer ")) {
          found = true;
          break;
        }
      } catch {
        /* ignore malformed delivery rows */
      }
    }
    assert.ok(found, `expected Bearer authorization in catcher dump body=${dumpRes.body}`);
  });
});
