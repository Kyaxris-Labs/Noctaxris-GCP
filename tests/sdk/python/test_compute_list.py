"""Compute / Run / Artifact Registry / Workflows / App Engine / Build / Functions / Scheduler / Tasks / Eventarc HTTP smokes."""

from __future__ import annotations

import json

import pytest

from conftest import do_json, project_id, require_ready, require_token


def test_list_compute_instances_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json(
        "GET",
        f"{ep}/compute/v1/projects/{project}/zones/us-central1-a/instances",
        token,
    )
    assert status == 200, body
    parsed = json.loads(body)
    assert parsed.get("kind") == "compute#instanceList", body
    assert "items" in parsed, body


def test_list_cloud_run_services_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json(
        "GET",
        f"{ep}/v2/projects/{project}/locations/us-central1/services",
        token,
    )
    assert status == 200, body
    parsed = json.loads(body)
    assert "services" in parsed, body


def test_list_artifact_registry_repositories_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json(
        "GET",
        f"{ep}/v1/projects/{project}/locations/us-central1/repositories",
        token,
    )
    assert status == 200, body
    parsed = json.loads(body)
    assert "repositories" in parsed, body


def test_list_workflows_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json(
        "GET",
        f"{ep}/v1/projects/{project}/locations/us-central1/workflows",
        token,
    )
    assert status == 200, body
    parsed = json.loads(body)
    assert "workflows" in parsed, body


def test_get_app_engine_app_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json("GET", f"{ep}/v1/apps/{project}", token)
    if status == 404:
        pytest.skip("App Engine app not created; soft-skip get app smoke")
    assert status == 200, body
    parsed = json.loads(body)
    if parsed.get("id"):
        assert parsed["id"] == project, body


def test_list_cloud_build_builds_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json("GET", f"{ep}/v1/projects/{project}/builds", token)
    assert status == 200, body
    parsed = json.loads(body)
    assert "builds" in parsed, body


def test_list_cloud_functions_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json(
        "GET",
        f"{ep}/v2/projects/{project}/locations/us-central1/functions",
        token,
    )
    assert status == 200, body
    parsed = json.loads(body)
    assert "functions" in parsed, body


def test_list_cloud_scheduler_jobs_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json(
        "GET",
        f"{ep}/v1/projects/{project}/locations/us-central1/jobs",
        token,
    )
    assert status == 200, body
    parsed = json.loads(body)
    assert "jobs" in parsed, body


def test_list_cloud_tasks_queues_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json(
        "GET",
        f"{ep}/v2/projects/{project}/locations/us-central1/queues",
        token,
    )
    assert status == 200, body
    parsed = json.loads(body)
    assert "queues" in parsed, body


def test_list_eventarc_channels_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json(
        "GET",
        f"{ep}/v1/projects/{project}/locations/us-central1/channels",
        token,
    )
    assert status == 200, body
    parsed = json.loads(body)
    assert "channels" in parsed, body
