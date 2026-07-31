"""Shared soft-skip helpers for Noctaxris-GCP Python SDK smokes."""

from __future__ import annotations

import json
import os
import time
import urllib.error
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


def truthy_env(name: str) -> bool:
    v = os.environ.get(name, "").strip()
    return v == "1" or v.lower() == "true"


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
