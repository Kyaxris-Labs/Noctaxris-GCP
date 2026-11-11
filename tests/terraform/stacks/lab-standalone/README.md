# Standalone project

Opt-in. This stack is not in the default `STACKS` list. Operators run it with `STACK=lab-standalone`.

The provider `access_token` comes from `TF_VAR_access_token`. Set that to the `accessToken` field from the seed response. `tests/terraform/run.sh` still exports `GOOGLE_OAUTH_ACCESS_TOKEN` from `NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN`. Provider configuration takes precedence over that environment variable (hashicorp/google >= 4.0, and this stack requires >= 5.0), so the apply uses the seeded standalone project token.

`project` has no default. `run.sh` passes `-var=project` from `NOCTAXRIS_GCP_PROJECT`. Export the seed `projectId` as that variable before apply. The bucket is created in `var.project`.

Start the API with `NOCTAXRIS_GCP_LAB_STANDALONE=1`. Seed with the root bearer, then export the returned access token and project id:

```bash
curl -s -H "Authorization: Bearer $NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"standalone-demo"}' \
  "$NOCTAXRIS_GCP_ENDPOINT/_noctaxris-gcp/lab/projects:seedStandalone"

export TF_VAR_access_token="<accessToken>"
export NOCTAXRIS_GCP_PROJECT="<projectId>"
STACK=lab-standalone bash tests/terraform/run.sh
```

`run.sh` still requires `NOCTAXRIS_GCP_ENDPOINT`, a ready API, and `NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN` (the seed call uses that root bearer). The stack does not read a token literal from this directory.
