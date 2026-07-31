"""GCS / Bigtable / Memorystore / Cloud SQL / Kafka / BigQuery / Spanner HTTP smokes."""

from __future__ import annotations

import json
import urllib.error
import urllib.parse
import urllib.request

from conftest import do_json, project_id, require_ready, require_token, unique_id


def test_list_buckets_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    q = urllib.parse.urlencode({"project": project})
    status, body = do_json("GET", f"{ep}/storage/v1/b?{q}", token)
    assert status == 200, body
    parsed = json.loads(body)
    assert parsed.get("kind") == "storage#buckets", body


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


def test_list_cloud_sql_instances_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json("GET", f"{ep}/sql/v1/projects/{project}/instances", token)
    assert status == 200, body
    parsed = json.loads(body)
    assert "items" in parsed, body


def test_list_managed_kafka_clusters_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json(
        "GET",
        f"{ep}/v1/projects/{project}/locations/us-central1/clusters",
        token,
    )
    assert status == 200, body
    parsed = json.loads(body)
    assert "clusters" in parsed, body


def test_list_bigquery_datasets_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json("GET", f"{ep}/bigquery/v2/projects/{project}/datasets", token)
    assert status == 200, body
    parsed = json.loads(body)
    assert "datasets" in parsed, body


def test_list_spanner_instances_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json("GET", f"{ep}/v1/projects/{project}/instances", token)
    assert status == 200, body
    parsed = json.loads(body)
    assert "instances" in parsed, body
