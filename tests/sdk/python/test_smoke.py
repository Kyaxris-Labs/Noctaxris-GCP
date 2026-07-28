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


def test_list_dns_managed_zones_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json("GET", f"{ep}/dns/v1/projects/{project}/managedZones", token)
    assert status == 200, body
    parsed = json.loads(body)
    assert "managedZones" in parsed, body


def test_list_bigtable_instances_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json("GET", f"{ep}/v2/projects/{project}/instances", token)
    assert status == 200, body
    parsed = json.loads(body)
    assert "instances" in parsed, body


def test_list_memorystore_instances_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json(
        "GET",
        f"{ep}/v1/projects/{project}/locations/us-central1/instances",
        token,
    )
    assert status == 200, body
    parsed = json.loads(body)
    assert "instances" in parsed, body


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


def test_list_cloud_armor_security_policies_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json(
        "GET",
        f"{ep}/compute/v1/projects/{project}/global/securityPolicies",
        token,
    )
    assert status == 200, body
    parsed = json.loads(body)
    assert parsed.get("kind") == "compute#securityPolicyList", body
    assert "items" in parsed, body


def test_list_certificate_manager_certificates_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json(
        "GET",
        f"{ep}/v1/projects/{project}/locations/global/certificates",
        token,
    )
    assert status == 200, body
    parsed = json.loads(body)
    assert "certificates" in parsed, body


def test_list_filestore_instances_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json(
        "GET",
        f"{ep}/file/v1/projects/{project}/locations/us-central1/instances",
        token,
    )
    assert status == 200, body
    parsed = json.loads(body)
    assert "instances" in parsed, body


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


def test_gcs_generate_signed_url_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()
    bucket = unique_id("sdk-signed")
    bucket_path = f"{ep}/storage/v1/b/{urllib.parse.quote(bucket, safe='')}"

    status, body = do_json(
        "POST",
        f"{ep}/storage/v1/b?project={urllib.parse.quote(project)}",
        token,
        {"name": bucket},
    )
    assert status == 200, body
    try:
        status, body = do_json(
            "POST",
            f"{bucket_path}/o/smoke.txt:generateSignedUrl",
            token,
            {"method": "GET", "expires": 600, "alt": "media"},
        )
        assert status == 200, body
        parsed = json.loads(body)
        assert parsed.get("signedUrl"), body
    finally:
        do_json("DELETE", bucket_path, token)


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


def test_gcs_retention_delete_deny_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()
    bucket = unique_id("sdk-retain")
    bucket_path = f"{ep}/storage/v1/b/{urllib.parse.quote(bucket, safe='')}"
    object_path = f"{bucket_path}/o/{urllib.parse.quote('held.txt', safe='')}"

    status, body = do_json(
        "POST",
        f"{ep}/storage/v1/b?project={urllib.parse.quote(project)}",
        token,
        {"name": bucket},
    )
    assert status == 200, body
    try:
        status, body = do_json(
            "PATCH",
            bucket_path,
            token,
            {"retentionPolicy": {"retentionPeriod": "3600"}},
        )
        assert status == 200, body

        up_url = (
            f"{ep}/upload/storage/v1/b/{urllib.parse.quote(bucket, safe='')}/o"
            f"?uploadType=media&name={urllib.parse.quote('held.txt')}"
        )
        up_req = urllib.request.Request(
            up_url,
            data=b"held",
            headers={"Authorization": f"Bearer {token}", "Content-Type": "text/plain"},
            method="POST",
        )
        try:
            with urllib.request.urlopen(up_req, timeout=5) as resp:
                up_status, up_body = resp.status, resp.read().decode()
        except urllib.error.HTTPError as err:
            up_status, up_body = err.code, err.read().decode(errors="replace")
        assert up_status == 200, up_body

        status, body = do_json("DELETE", object_path, token)
        assert status != 200, body
        parsed = json.loads(body)
        assert parsed.get("error", {}).get("status") == "FAILED_PRECONDITION", body
    finally:
        do_json("DELETE", object_path, token)
        do_json("DELETE", bucket_path, token)
