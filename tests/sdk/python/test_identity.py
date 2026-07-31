"""Identity / CRM / STS / Service Usage HTTP smokes."""

from __future__ import annotations

import json
import urllib.error
import urllib.parse
import urllib.request

from conftest import do_json, project_id, require_ready, require_token, unique_id


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


def test_iam_generate_access_token_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()
    account_id = unique_id("sdksa")
    if len(account_id) > 30:
        account_id = account_id[:30].rstrip("-")
    email = f"{account_id}@{project}.iam.gserviceaccount.com"
    sa_path = f"{ep}/v1/projects/{project}/serviceAccounts/{email}"

    status, body = do_json(
        "POST",
        f"{ep}/v1/projects/{project}/serviceAccounts",
        token,
        {
            "accountId": account_id,
            "serviceAccount": {"displayName": "sdk smoke"},
        },
    )
    assert status == 200, body
    try:
        status, body = do_json(
            "POST",
            f"{sa_path}:generateAccessToken",
            token,
            {
                "scope": ["https://www.googleapis.com/auth/cloud-platform"],
                "lifetime": "3600s",
            },
        )
        assert status == 200, body
        parsed = json.loads(body)
        assert parsed.get("accessToken"), body
    finally:
        do_json("DELETE", sa_path, token)


def test_sts_token_exchange_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()
    pool_id = unique_id("sdk-sts-pool")
    if len(pool_id) > 32:
        pool_id = pool_id[:32].rstrip("-")
    provider_id = "oidc"
    pool_base = f"{ep}/v1/projects/{project}/locations/global/workloadIdentityPools/{pool_id}"
    provider_name = (
        f"projects/{project}/locations/global/workloadIdentityPools/{pool_id}/providers/{provider_id}"
    )

    q = urllib.parse.urlencode({"workloadIdentityPoolId": pool_id})
    status, body = do_json(
        "POST",
        f"{ep}/v1/projects/{project}/locations/global/workloadIdentityPools?{q}",
        token,
        {"displayName": "sdk sts pool"},
    )
    assert status == 200, body
    try:
        pq = urllib.parse.urlencode({"workloadIdentityPoolProviderId": provider_id})
        status, body = do_json(
            "POST",
            f"{pool_base}/providers?{pq}",
            token,
            {"displayName": "sdk oidc", "oidc": {"issuerUri": "https://example.com"}},
        )
        assert status == 200, body
        try:
            form = urllib.parse.urlencode(
                {
                    "grant_type": "urn:ietf:params:oauth:grant-type:token-exchange",
                    "audience": f"//iam.googleapis.com/{provider_name}",
                    "subject_token": "sdk-sts-sub",
                    "subject_token_type": "urn:ietf:params:oauth:token-type:jwt",
                }
            )
            req = urllib.request.Request(
                f"{ep}/v1/token",
                data=form.encode(),
                headers={"Content-Type": "application/x-www-form-urlencoded"},
                method="POST",
            )
            try:
                with urllib.request.urlopen(req, timeout=5) as resp:
                    status, body = resp.status, resp.read().decode()
            except urllib.error.HTTPError as err:
                status, body = err.code, err.read().decode(errors="replace")
            assert status == 200, body
            parsed = json.loads(body)
            assert parsed.get("access_token"), body
            assert parsed.get("token_type") == "Bearer", body
        finally:
            do_json("DELETE", f"{pool_base}/providers/{provider_id}", token)
    finally:
        do_json("DELETE", pool_base, token)


def test_list_service_usage_services_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json("GET", f"{ep}/v1/projects/{project}/services", token)
    assert status == 200, body
    parsed = json.loads(body)
    assert "services" in parsed, body
