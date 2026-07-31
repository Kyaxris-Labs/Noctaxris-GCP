"""DNS / Armor / Certificate Manager / Filestore / GKE / LB / CDN HTTP smokes."""

from __future__ import annotations

import json

from conftest import do_json, project_id, require_ready, require_token


def test_list_dns_managed_zones_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json("GET", f"{ep}/dns/v1/projects/{project}/managedZones", token)
    assert status == 200, body
    parsed = json.loads(body)
    assert "managedZones" in parsed, body


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


def test_list_gke_clusters_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json(
        "GET",
        f"{ep}/container/v1/projects/{project}/locations/us-central1/clusters",
        token,
    )
    assert status == 200, body
    parsed = json.loads(body)
    assert "clusters" in parsed, body


def test_list_load_balancing_backend_services_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json(
        "GET",
        f"{ep}/compute/v1/projects/{project}/global/backendServices",
        token,
    )
    assert status == 200, body
    parsed = json.loads(body)
    assert parsed.get("kind") == "compute#backendServiceList", body
    assert "items" in parsed, body


def test_list_cdn_distributions_smoke() -> None:
    ep = require_ready()
    token = require_token()
    project = project_id()

    status, body = do_json(
        "GET",
        f"{ep}/v1/projects/{project}/global/distributions",
        token,
    )
    assert status == 200, body
    parsed = json.loads(body)
    assert "distributions" in parsed, body
