"""Soft-skip HTTP smoke against a running Noctaxris-GCP process."""

from __future__ import annotations

import json
import os
import urllib.error
import urllib.request

import pytest

READY_PATH = "/_noctaxris-gcp/ready"


def endpoint() -> str | None:
    ep = os.environ.get("NOCTAXRIS_GCP_ENDPOINT", "").strip().rstrip("/")
    return ep or None


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


def test_ready_and_get_project_smoke() -> None:
    ep = require_ready()
    token = os.environ.get("NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN", "").strip()
    if not token:
        pytest.skip("NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN unset; soft-skip authenticated smoke")
    project = os.environ.get("NOCTAXRIS_GCP_PROJECT", "").strip() or "noctaxris-gcp-local"

    req = urllib.request.Request(
        f"{ep}/v3/projects/{project}",
        headers={"Authorization": f"Bearer {token}"},
        method="GET",
    )
    with urllib.request.urlopen(req, timeout=5) as resp:
        body = resp.read().decode()
        assert resp.status == 200, body
    parsed = json.loads(body)
    assert parsed.get("projectId") == project, body
