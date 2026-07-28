# Firebase Auth (Identity Toolkit)

Lab Identity Toolkit REST for email/password auth and admin user CRUD.

## Status

**lab** — signUp / signInWithPassword / lookup / update / delete, admin user CRUD, unsigned custom tokens.

## Wire protocol

Client methods (no Bearer required; emulator-shaped):

| Method | Path |
|--------|------|
| `POST` | `/identitytoolkit.googleapis.com/v1/accounts:signUp` |
| `POST` | `/identitytoolkit.googleapis.com/v1/accounts:signInWithPassword` |
| `POST` | `/identitytoolkit.googleapis.com/v1/accounts:lookup` |
| `POST` | `/identitytoolkit.googleapis.com/v1/accounts:update` |
| `POST` | `/identitytoolkit.googleapis.com/v1/accounts:delete` |
| `POST` | `/identitytoolkit.googleapis.com/v1/accounts:signInWithCustomToken` |

Admin (Bearer required):

| Method | Path |
|--------|------|
| `POST` | `/identitytoolkit.googleapis.com/v1/projects/{project}/accounts` |
| `GET` | `/identitytoolkit.googleapis.com/v1/projects/{project}/accounts` |
| `GET` | `/identitytoolkit.googleapis.com/v1/projects/{project}/accounts/{localId}` |
| `PATCH` | `/identitytoolkit.googleapis.com/v1/projects/{project}/accounts/{localId}` |
| `DELETE` | `/identitytoolkit.googleapis.com/v1/projects/{project}/accounts/{localId}` |
| `POST` | `/identitytoolkit.googleapis.com/v1/projects/{project}/accounts:createCustomToken` |

Custom tokens and id tokens are **unsigned lab JWTs** (`alg: none`, empty signature segment). Do not treat them as production credentials.

## Client configuration

```bash
export FIREBASE_AUTH_EMULATOR_HOST=127.0.0.1:4588
```

Many Firebase Admin / client SDKs honor `FIREBASE_AUTH_EMULATOR_HOST` and talk Identity Toolkit paths on that host. Send `targetProjectId` (or rely on the seeded default project) when the lab is multi-project-less.

Admin calls still need `Authorization: Bearer <token>`.

## Authz (admin)

- `firebaseauth.users.create|get|list|update|delete`

## Deferred depth

- Phone / OAuth / SAML / OIDC providers
- MFA, blocking functions, tenant management
- Signed JWTs / real Google public keys
- Session cookies with real cookies

## Verification / CLI smoke

```bash
go test ./internal/server/ -run FirebaseAuth -count=1
curl -s -H "Content-Type: application/json" \
  -d '{"email":"a@example.com","password":"secret123","returnSecureToken":true}' \
  http://127.0.0.1:4588/identitytoolkit.googleapis.com/v1/accounts:signUp
```
