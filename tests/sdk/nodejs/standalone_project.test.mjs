import { test } from "node:test";
import assert from "node:assert/strict";
import { doJSON, projectID, requireReady, requireToken } from "./helpers.mjs";

const seedName = "standalone-sdk-node";

function parseSeed(body) {
  try {
    return JSON.parse(body);
  } catch {
    assert.fail("seed response was not JSON");
  }
}

function requireAccessToken(value, root) {
  if (typeof value !== "string" || value.length === 0) {
    assert.fail("seed response missing accessToken");
  }
  if (value === root) {
    assert.fail("seed access token must differ from the root token");
  }
  return value;
}

test("standalone project seed smoke", async (t) => {
  const ep = await requireReady(t);
  if (!ep) return;
  const root = requireToken(t);
  if (!root) return;

  const seeded = await doJSON("POST", `${ep}/_noctaxris-gcp/lab/projects:seedStandalone`, root, {
    name: seedName,
  });
  if (seeded.status === 404) {
    t.skip("standalone project seed returned 404; NOCTAXRIS_GCP_LAB_STANDALONE is off");
    return;
  }
  assert.equal(seeded.status, 200, `seed status=${seeded.status}`);
  const parsed = parseSeed(seeded.body);
  const accessToken = requireAccessToken(parsed.accessToken, root);
  const home = parsed.projectId;
  if (typeof home !== "string" || home.length === 0) {
    assert.fail("seed response missing projectId");
  }

  const homeGet = await doJSON("GET", `${ep}/v3/projects/${encodeURIComponent(home)}`, accessToken);
  assert.equal(homeGet.status, 200, `get home project status=${homeGet.status} body=${homeGet.body}`);

  const def = projectID();
  const defGet = await doJSON("GET", `${ep}/v3/projects/${encodeURIComponent(def)}`, accessToken);
  assert.equal(defGet.status, 403, `get default project status=${defGet.status} body=${defGet.body}`);

  const parent = `projects/${home}`;
  const listed = await doJSON(
    "GET",
    `${ep}/v3/projects?parent=${encodeURIComponent(parent)}`,
    accessToken,
  );
  assert.equal(listed.status, 200, `list projects status=${listed.status} body=${listed.body}`);
  const list = JSON.parse(listed.body);
  const projects = list.projects;
  assert.ok(Array.isArray(projects), `list projects body=${listed.body}`);
  const ids = projects.map((row) => row && row.projectId);
  assert.ok(ids.includes(home), `list under ${parent} missing home project; ids=${ids.join(",")}`);
  assert.ok(!ids.includes(def), `list under ${parent} included default project ${def}`);

  const again = await doJSON("POST", `${ep}/_noctaxris-gcp/lab/projects:seedStandalone`, root, {
    name: seedName,
  });
  assert.equal(again.status, 200, `reseed status=${again.status}`);
  const repeated = parseSeed(again.body);
  if (repeated.accessToken !== accessToken) {
    assert.fail("repeat seed must return the same access token");
  }
});
