# Compute Engine (and VPC / Firewall)

Lab Compute Engine API v1 metadata theatre: instances, VPC networks, regional
subnetworks, firewall rules, and a fixed Images catalog for Terraform
`ResolveImage`. No nested VMs, DinD, qemu, or host `docker.sock`.

## Status

**lab** — CRUD lite stores JSON metadata; instance `status` theatre
(`RUNNING` / `TERMINATED`); instance `metadata` map stored and returned on get;
firewall `:validate` allow/deny eval lite and `:testIamPermissions`; global
Images list/get/family stubs return canned `READY` images. Mutating
calls return a completed `compute#operation`.

## Wire protocol

REST on the shared listener (`http://127.0.0.1:4588`), googleapis-shaped
`/compute/v1/...` paths.

### Instances

| Method | Path |
|--------|------|
| `GET` | `/compute/v1/projects/{p}/zones/{z}/instances` |
| `POST` | `/compute/v1/projects/{p}/zones/{z}/instances` |
| `GET` | `/compute/v1/projects/{p}/zones/{z}/instances/{instance}` |
| `PATCH` | `/compute/v1/projects/{p}/zones/{z}/instances/{instance}` |
| `DELETE` | `/compute/v1/projects/{p}/zones/{z}/instances/{instance}` |
| `POST` | `/compute/v1/projects/{p}/zones/{z}/instances/{instance}/stop` |
| `POST` | `/compute/v1/projects/{p}/zones/{z}/instances/{instance}/start` |
| `POST` | `/compute/v1/projects/{p}/zones/{z}/instances/{instance}/reset` |

Insert body requires `name`. Optional `machineType`, `networkInterfaces`,
`metadata` (string map or `{items:[{key,value}]}`). Default status after insert
is `RUNNING`. `stop` sets `TERMINATED`; `start` / `reset` set `RUNNING`. Path
actions are parsed from a trailing wildcard (ServeMux-safe; colon-style
`instance:stop` also accepted).

### Networks / subnetworks / firewalls

| Method | Path |
|--------|------|
| `GET`/`POST` | `/compute/v1/projects/{p}/global/networks` |
| `GET`/`PATCH`/`DELETE` | `/compute/v1/projects/{p}/global/networks/{network}` |
| `GET`/`POST` | `/compute/v1/projects/{p}/regions/{r}/subnetworks` |
| `GET`/`PATCH`/`DELETE` | `/compute/v1/projects/{p}/regions/{r}/subnetworks/{subnetwork}` |
| `GET`/`POST` | `/compute/v1/projects/{p}/global/firewalls` |
| `GET`/`PATCH`/`DELETE` | `/compute/v1/projects/{p}/global/firewalls/{firewall}` |
| `POST` | `/compute/v1/projects/{p}/global/firewalls/{firewall}:validate` |
| `POST` | `/compute/v1/projects/{p}/global/firewalls/{firewall}:testIamPermissions` |

Create bodies require `name`. Subnetworks store `network` and `ipCidrRange`.
Extra JSON fields are kept in metadata.

`:validate` body: `{"sourceIp":"10.1.2.3","protocol":"tcp","port":80}`. Response
includes `matched`, `allowed`, `action` (`ALLOW`/`DENY`/`NONE`), and `reason`.
Evaluation is single-rule lite against `sourceRanges` + `allowed`/`denied`
(fail-closed on miss). `:testIamPermissions` returns granted permission strings
for the caller on the project.

### Images

| Method | Path |
|--------|------|
| `GET` | `/compute/v1/projects/{p}/global/images` |
| `GET` | `/compute/v1/projects/{p}/global/images/{image}` |
| `GET` | `/compute/v1/projects/{p}/global/images/family/{family}` |

Fixed theatre catalog (same set for any `{p}`, including `debian-cloud` /
`ubuntu-os-cloud` / `cos-cloud` style project IDs used by Terraform ResolveImage):

| Family | Image name | Status |
|--------|------------|--------|
| `debian-12` | `debian-12-bookworm-v20240701` | `READY` |
| `ubuntu-2204-lts` | `ubuntu-2204-jammy-v20240701` | `READY` |
| `cos-stable` | `cos-stable-117-18613-0-0` | `READY` |

Get by name also accepts the family string as an alias. Responses include
`selfLink`, `name`, `family`, and `status`.

## Authz

Checked on `projects/{project}`:

- `compute.instances.create|get|list|delete|update|stop|start|reset`
- `compute.networks.create|get|list|delete|update`
- `compute.subnetworks.create|get|list|delete|update`
- `compute.firewalls.create|get|list|delete|update`
- `compute.images.get|list`

Seeded Service Usage: `compute.googleapis.com`.

## Emulator limits

- Metadata only; never starts a VM or attaches a real NIC
- No disks, instance groups (HTTP(S) Load Balancing lite is a separate lab service when registered)
- Images are a fixed canned set (no import/create/delete)
- Insert/delete/stop/start/reset return completed Operations (no poll queue)
- Firewall `:validate` evaluates one rule only (no priority chain across rules)
- No guest metadata server (`metadata.google.internal`) yet

## Deferred depth

- Attached disks, snapshots, custom image import, MIGs, backend services
- Operations get/list and async progress
- Private Google Access / Cloud NAT / routes CRUD depth
- Instance metadata server path theatre

## Verification / CLI smoke

```bash
go test ./internal/services/compute/ ./internal/store/ ./internal/server/ -run 'GCE|Compute|InstanceMetadata|Firewall|Image' -count=1
TOKEN=$NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/compute/v1/projects/noctaxris-gcp-local/zones/us-central1-a/instances" \
  -d '{"name":"lab-vm","machineType":"zones/us-central1-a/machineTypes/e2-micro","metadata":{"role":"lab"}}'
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:4588/compute/v1/projects/noctaxris-gcp-local/global/images/family/debian-12"
curl -s -H "Authorization: Bearer $TOKEN" \
  -X POST "http://127.0.0.1:4588/compute/v1/projects/noctaxris-gcp-local/global/firewalls" \
  -d '{"name":"allow-http","network":"global/networks/default","sourceRanges":["0.0.0.0/0"],"allowed":[{"IPProtocol":"tcp","ports":["80"]}]}'
curl -s -H "Authorization: Bearer $TOKEN" -X POST \
  "http://127.0.0.1:4588/compute/v1/projects/noctaxris-gcp-local/global/firewalls/allow-http:validate" \
  -d '{"sourceIp":"203.0.113.10","protocol":"tcp","port":80}'
```

```bash
gcloud config set api_endpoint_overrides/compute http://127.0.0.1:4588/
STACK=lab-compute bash tests/terraform/run.sh
# compute_custom_endpoint = "http://127.0.0.1:4588/compute/v1/"
# Images ResolveImage gap closed; google_compute_instance still needs disks/boot
```
