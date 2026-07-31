"""Pub/Sub HTTP smokes."""

from __future__ import annotations

import base64
import json
import urllib.error

import pytest

from conftest import do_json, project_id, require_ready, require_token, unique_id


def test_pubsub_create_and_list_topics_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()
    topic_id = unique_id("sdk-topic")
    topic_path = f"{ep}/v1/projects/{project}/topics/{topic_id}"

    status, body = do_json("PUT", topic_path, token, {})
    assert status == 200, body
    try:
        status, body = do_json("GET", f"{ep}/v1/projects/{project}/topics", token)
        assert status == 200, body
        parsed = json.loads(body)
        assert "topics" in parsed, body
    finally:
        do_json("DELETE", topic_path, token)


def test_pubsub_oidc_push_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()
    topic_id = unique_id("sdk-oidc-topic")
    sub_id = unique_id("sdk-oidc-sub")
    topic_path = f"{ep}/v1/projects/{project}/topics/{topic_id}"
    sub_path = f"{ep}/v1/projects/{project}/subscriptions/{sub_id}"
    catcher = "http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/sdk-oidc-push"
    sa_email = f"push-sa@{project}.iam.gserviceaccount.com"
    audience = "https://example.com/sdk-oidc"

    status, body = do_json("PUT", topic_path, token, {})
    assert status == 200, body
    try:
        status, body = do_json(
            "PUT",
            sub_path,
            token,
            {
                "topic": f"projects/{project}/topics/{topic_id}",
                "ackDeadlineSeconds": 10,
                "pushConfig": {
                    "pushEndpoint": catcher,
                    "oidcToken": {
                        "serviceAccountEmail": sa_email,
                        "audience": audience,
                    },
                },
            },
        )
        assert status == 200, body
        created = json.loads(body)
        pc = created.get("pushConfig") or {}
        oidc = pc.get("oidcToken") or {}
        if (
            pc.get("pushEndpoint") != catcher
            or oidc.get("serviceAccountEmail") != sa_email
            or oidc.get("audience") != audience
        ):
            pytest.fail(f"oidcToken round-trip failed: {created.get('pushConfig')}")

        payload = base64.b64encode(b"sdk-oidc-ping").decode()
        status, body = do_json(
            "POST",
            f"{topic_path}:publish",
            token,
            {"messages": [{"data": payload}]},
        )
        assert status == 200, body

        try:
            status, dump = do_json("GET", f"{ep}/_noctaxris-gcp/http-catcher", token)
        except (urllib.error.URLError, TimeoutError, OSError) as err:
            pytest.skip(
                f"lab catcher dump unavailable (status=0 err={err}); soft-skip Authorization assert "
                "(unit TestPubSubOIDCPushCatcher covers Bearer JWT)"
            )
        if status != 200:
            pytest.skip(
                f"lab catcher dump unavailable (status={status}); soft-skip Authorization assert "
                "(unit TestPubSubOIDCPushCatcher covers Bearer JWT)"
            )
        parsed = json.loads(dump)
        deliveries = parsed.get("deliveries")
        if not isinstance(deliveries, list) or len(deliveries) == 0:
            pytest.skip("catcher dump empty; soft-skip Authorization assert")
        found = False
        for raw in reversed(deliveries):
            if not isinstance(raw, str):
                continue
            try:
                entry = json.loads(raw)
            except json.JSONDecodeError:
                continue
            authz = entry.get("authorization")
            if isinstance(authz, str) and authz.startswith("Bearer "):
                found = True
                break
        assert found, f"expected Bearer authorization in catcher dump body={dump}"
    finally:
        do_json("DELETE", sub_path, token)
        do_json("DELETE", topic_path, token)
