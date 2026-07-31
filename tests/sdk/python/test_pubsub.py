"""Pub/Sub HTTP smokes."""

from __future__ import annotations

import json

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
