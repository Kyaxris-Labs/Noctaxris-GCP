"""Logging / Monitoring / Dataflow / Vertex AI HTTP smokes."""

from __future__ import annotations

import json

from conftest import do_json, project_id, require_ready, require_token


def test_list_logging_sinks_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json("GET", f"{ep}/v2/projects/{project}/sinks", token)
    assert status == 200, body
    parsed = json.loads(body)
    assert "sinks" in parsed, body


def test_list_monitoring_alert_policies_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json("GET", f"{ep}/v3/projects/{project}/alertPolicies", token)
    assert status == 200, body
    parsed = json.loads(body)
    assert "alertPolicies" in parsed, body


def test_list_dataflow_jobs_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json(
        "GET",
        f"{ep}/v1b3/projects/{project}/locations/us-central1/jobs",
        token,
    )
    assert status == 200, body
    parsed = json.loads(body)
    assert "jobs" in parsed, body


def test_vertex_ai_generate_content_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json(
        "POST",
        f"{ep}/v1/projects/{project}/locations/us-central1/publishers/google/models/gemini-1.5-flash:generateContent",
        token,
        {
            "contents": [
                {"role": "user", "parts": [{"text": "sdk-smoke"}]},
            ],
        },
    )
    assert status == 200, body
    parsed = json.loads(body)
    assert "candidates" in parsed, body
