"""Nested DinD / fail-closed HTTP smokes."""

from __future__ import annotations

import urllib.parse

import pytest

from conftest import do_json, project_id, require_ready, require_token, truthy_env, unique_id


def test_nested_invoke_fail_closed_smoke() -> None:
    if not truthy_env("NOCTAXRIS_GCP_NESTED_INVOKE_FAIL_CLOSED"):
        pytest.skip("NOCTAXRIS_GCP_NESTED_INVOKE_FAIL_CLOSED unset; soft-skip nested fail-closed smoke")
    ep = require_ready()
    token = require_token()
    project = project_id()
    svc_id = unique_id("sdk-failclosed")
    if len(svc_id) > 49:
        svc_id = svc_id[:49].rstrip("-")
    base = f"{ep}/v2/projects/{project}/locations/us-central1/services"
    svc_path = f"{base}/{svc_id}"

    status, body = do_json(
        "POST",
        f"{base}?serviceId={urllib.parse.quote(svc_id)}",
        token,
        {"template": {"containers": [{"image": "demo"}]}},
    )
    assert status == 200, body
    try:
        status, body = do_json("POST", f"{svc_path}:invoke", token, {})
        if status == 200:
            if '"mode":"mock"' in body or '"ok":true' in body:
                pytest.skip(
                    f"server returned soft-fail/mock invoke (DOCKER_HOST empty or not fail-closed); soft-skip body={body}"
                )
            pytest.fail(f":invoke unexpectedly OK under fail-closed env body={body}")
        assert status >= 400, f":invoke want error status, got {status} body={body}"
        assert '"mode":"mock"' not in body, f":invoke should not soft-fail to mock under fail-closed, body={body}"
    finally:
        do_json("DELETE", svc_path, token)
