"""Standalone project seed HTTP smoke."""

from __future__ import annotations

import json
import urllib.parse

import pytest

from conftest import do_json, project_id, require_ready, require_token

SEED_NAME = "standalone-sdk-py"
SEED_PATH = "/_noctaxris-gcp/lab/projects:seedStandalone"


def _parse_seed(body: str) -> dict:
    try:
        parsed = json.loads(body)
    except json.JSONDecodeError:
        raise AssertionError("seed response was not JSON") from None
    if not isinstance(parsed, dict):
        raise AssertionError("seed response was not a JSON object")
    return parsed


def _require_access_token(value: object, root: str) -> str:
    if not isinstance(value, str) or not value:
        raise AssertionError("seed response missing accessToken")
    if value == root:
        raise AssertionError("seed access token must differ from the root token")
    return value


def test_standalone_project_seed_smoke() -> None:
    ep = require_ready()
    root = require_token()

    status, body = do_json("POST", f"{ep}{SEED_PATH}", root, {"name": SEED_NAME})
    if status == 404:
        pytest.skip("standalone project seed returned 404; NOCTAXRIS_GCP_LAB_STANDALONE is off")
    assert status == 200, f"seed status={status}"
    parsed = _parse_seed(body)
    access_token = _require_access_token(parsed.get("accessToken"), root)
    home = parsed.get("projectId")
    if not isinstance(home, str) or not home:
        raise AssertionError("seed response missing projectId")

    status, body = do_json("GET", f"{ep}/v3/projects/{urllib.parse.quote(home, safe='')}", access_token)
    assert status == 200, body

    default = project_id()
    status, body = do_json(
        "GET",
        f"{ep}/v3/projects/{urllib.parse.quote(default, safe='')}",
        access_token,
    )
    assert status == 403, body

    parent = f"projects/{home}"
    query = urllib.parse.urlencode({"parent": parent})
    status, body = do_json("GET", f"{ep}/v3/projects?{query}", access_token)
    assert status == 200, body
    listed = json.loads(body)
    projects = listed.get("projects")
    assert isinstance(projects, list), body
    ids = [row.get("projectId") if isinstance(row, dict) else None for row in projects]
    assert home in ids, f"list under {parent} missing home project; ids={ids}"
    assert default not in ids, f"list under {parent} included default project {default}"

    status, body = do_json("POST", f"{ep}{SEED_PATH}", root, {"name": SEED_NAME})
    assert status == 200, f"reseed status={status}"
    repeated = _parse_seed(body)
    if repeated.get("accessToken") != access_token:
        raise AssertionError("repeat seed must return the same access token")
