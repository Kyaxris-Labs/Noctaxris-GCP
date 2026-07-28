"""Soft-skip HTTP smoke against a running Noctaxris-GCP process."""

from __future__ import annotations

import base64
import json
import os
import time
import urllib.error
import urllib.parse
import urllib.request

import pytest

READY_PATH = "/_noctaxris-gcp/ready"


def endpoint() -> str | None:
    ep = os.environ.get("NOCTAXRIS_GCP_ENDPOINT", "").strip().rstrip("/")
    return ep or None


def project_id() -> str:
    return os.environ.get("NOCTAXRIS_GCP_PROJECT", "").strip() or "noctaxris-gcp-local"


def unique_id(prefix: str) -> str:
    return f"{prefix}-{time.time_ns()}"


def require_ready() -> str:
    ep = endpoint()
    if not ep:
        pytest.skip("NOCTAXRIS_GCP_ENDPOINT unset; soft-skip live smoke")
    url = f"{ep}{READY_PATH}"
    try:
        with urllib.request.urlopen(url, timeout=2) as resp:
            if resp.status != 200:
                pytest.skip(f"Noctaxris-GCP not ready at {ep}: status {resp.status}")
    except (urllib.error.URLError, TimeoutError, OSError) as err:
        pytest.skip(f"Noctaxris-GCP not reachable at {ep}: {err}")
    return ep


def require_token() -> str:
    token = os.environ.get("NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN", "").strip()
    if not token:
        pytest.skip("NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN unset; soft-skip authenticated smoke")
    return token


def do_json(method: str, url: str, token: str, body: dict | None = None) -> tuple[int, str]:
    data = None
    headers = {"Authorization": f"Bearer {token}"}
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=5) as resp:
            return resp.status, resp.read().decode()
    except urllib.error.HTTPError as err:
        return err.code, err.read().decode(errors="replace")


def test_ready_and_get_project_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json("GET", f"{ep}/v3/projects/{project}", token)
    assert status == 200, body
    parsed = json.loads(body)
    assert parsed.get("projectId") == project, body


def test_get_organization_smoke() -> None:
    ep = require_ready()
    token = require_token()

    status, body = do_json("GET", f"{ep}/v3/organizations/noctaxris-gcp-org", token)
    assert status == 200, body
    parsed = json.loads(body)
    assert parsed.get("name") == "organizations/noctaxris-gcp-org", body


def test_list_folders_smoke() -> None:
    ep = require_ready()
    token = require_token()

    parent = urllib.parse.quote("organizations/noctaxris-gcp-org", safe="")
    status, body = do_json("GET", f"{ep}/v3/folders?parent={parent}", token)
    assert status == 200, body
    parsed = json.loads(body)
    assert "folders" in parsed, body


def test_list_buckets_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    q = urllib.parse.urlencode({"project": project})
    status, body = do_json("GET", f"{ep}/storage/v1/b?{q}", token)
    assert status == 200, body
    parsed = json.loads(body)
    assert parsed.get("kind") == "storage#buckets", body


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
