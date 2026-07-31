"""Secret Manager / KMS HTTP smokes."""

from __future__ import annotations

import base64
import json
import urllib.parse

from conftest import do_json, project_id, require_ready, require_token, unique_id


def test_secret_manager_create_access_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()
    secret_id = unique_id("sdk-secret")
    base = f"{ep}/v1/projects/{project}/secrets/{secret_id}"

    q = urllib.parse.urlencode({"secretId": secret_id})
    status, body = do_json(
        "POST",
        f"{ep}/v1/projects/{project}/secrets?{q}",
        token,
        {"replication": {"automatic": {}}},
    )
    assert status == 200, body
    try:
        payload = base64.b64encode(b"sdk-smoke").decode()
        status, body = do_json("POST", f"{base}:addVersion", token, {"payload": {"data": payload}})
        assert status == 200, body

        status, body = do_json("GET", f"{base}/versions/latest:access", token)
        assert status == 200, body
        parsed = json.loads(body)
        got = base64.b64decode(parsed.get("payload", {}).get("data", "")).decode()
        assert got == "sdk-smoke", body
    finally:
        do_json("DELETE", base, token)


def test_list_kms_key_rings_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json(
        "GET",
        f"{ep}/v1/projects/{project}/locations/global/keyRings",
        token,
    )
    assert status == 200, body
    parsed = json.loads(body)
    assert "keyRings" in parsed, body
