# AGENTS.md — Noctaxris-GCP

Guidance for coding agents in this repository. Follow unless a maintainer says otherwise.

## Product bar

- Go GCP-faithful local emulator built from scratch. Secure by default. Loopback `:4588`.
- No host `docker.sock`. Nested DinD opt-in via `compose.engine.yaml`. Nested port publish default-off.
- Authn: Bearer OAuth (injected root SA + access token). Authz: IAM `Evaluate` / `EvaluateAny` deny-by-default. Root SA bypass is intentional and documented.
- Prefer proper service packages over deepen stubs. Official cloud APIs only (gcloud, official SDKs, Terraform).
- Public docs under `docs/`. No roadmap labels in public surfaces.
- Module path: `github.com/Kyaxris-Labs/Noctaxris-GCP`. Hub image: `kyaxris/noctaxris-gcp`. Default project: `noctaxris-gcp-local`.

## JWT and crypto

- Use `internal/kernel/jwtutil` backed by `github.com/go-jose/go-jose/v4`.
- Identity Toolkit custom/id tokens, Pub/Sub push OIDC, and lab JWTs must use jose. Reject `alg=none`.
- No new hand-rolled compact JWT sign/verify.

## CEL

- Use `internal/kernel/celutil` backed by `cel.dev/cel-go` for:
  - IAM condition expressions (`request.time` and documented vars)
  - WIF / STS `attributeMapping` and `attributeCondition`
  - Cloud Armor CEL match expressions
- Fail closed on compile/eval errors or unknown variables. Do not leave “CEL not evaluated” theatre on marketed paths.

## Authz attach (DRY)

- Service-account attach: `restlab.RequireServiceAccountActAs` (or equivalent) on create, retry, run, and trigger paths that mint or run as an SA.
- Never grant TokenCreator-class perms via `roles/iam.serviceAccountAdmin` or `roles/iam.securityAdmin` prefix wildcards. Use explicit permission sets.
- Custom roles with `stage=DISABLED` must not grant.
- Firestore/Datastore document paths must bind to the authorized project.
- Toolkit / Firebase principals: namespace localIds so they cannot collide with `wif:` or SA emails.
- Marketed APIs: Service Usage enablement and VPC-SC checks via shared Require helpers where enforced.

## Service-add checklist (auditability)

1. **Authn** — Bearer required, or documented public path (`/lb/`, `/cdn/`, metadata, health, JWKS).
2. **Authz** — concrete permission(s); project vs resource-scoped Evaluate as appropriate.
3. **actAs / TokenCreator** — if a service account email or SA resource is accepted, require actAs (and TokenCreator perms where Credentials API is used).
4. **Service Usage / VPC-SC** — gate create/mutate on marketed surfaces when those controls are in scope.
5. **Secrets** — no secret payload on metadata-only roles; no key echo on list.
6. **Docs** — `docs/services/<svc>.md` with deferred depth + verification; update `docs/services/index.md` and `docs/security-defaults.md`.
7. **Tests** — positive allow, negative deny, boundary (wrong project path, empty mapping), error-guessing (DISABLED role, alg=none, missing actAs). Soft-skip SDK/TF when `NOCTAXRIS_GCP_ENDPOINT` unset.
8. **Coverage** — keep `./internal/...` statement coverage at or above 75%. Feature-oriented test names. Do not commit coverage artifacts.

## Libraries

- go-jose for JWT; cel-go for CEL; Google API/gRPC protos for wire shapes.
- Do not replace the in-process IAM evaluator with OPA/Casbin or live GCP IAM calls at runtime.
- Nested Engine: `github.com/moby/moby/client`. Latest deps; pin Actions majors.

## Docs and release

- Align CHANGELOG / README / release docs with Noctaxris siblings. No tag/push unless asked.
- `ci-required` before Hub publish.

## Privacy

- Do not put personal machine paths, home directories, or private workspace names in commits, docs, tests, comments, or CI logs.
- Keep public prose limited to this product and its cloud APIs.
