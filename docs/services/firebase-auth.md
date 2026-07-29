# Firebase Auth (Identity Toolkit)

Lab Identity Toolkit REST for email/password auth, password-reset OOB codes, admin user CRUD, custom claims, and unsigned JWT verify.

## Status

**lab** — signUp / signInWithPassword / lookup / update / delete, sendOobCode / resetPassword, admin user CRUD with pagination, setCustomUserClaims, verifyIdToken, unsigned custom tokens.

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
| `POST` | `/identitytoolkit.googleapis.com/v1/accounts:sendOobCode` |
| `POST` | `/identitytoolkit.googleapis.com/v1/accounts:resetPassword` |

Admin (Bearer required):

| Method | Path |
|--------|------|
| `POST` | `/identitytoolkit.googleapis.com/v1/projects/{project}/accounts` |
| `GET` | `/identitytoolkit.googleapis.com/v1/projects/{project}/accounts` (`maxResults`, `nextPageToken` / `pageToken`) |
| `GET` | `/identitytoolkit.googleapis.com/v1/projects/{project}/accounts/{localId}` |
| `PATCH` | `/identitytoolkit.googleapis.com/v1/projects/{project}/accounts/{localId}` |
| `DELETE` | `/identitytoolkit.googleapis.com/v1/projects/{project}/accounts/{localId}` |
| `POST` | `/identitytoolkit.googleapis.com/v1/projects/{project}/accounts:createCustomToken` |
| `POST` | `/identitytoolkit.googleapis.com/v1/projects/{project}/accounts:setCustomUserClaims` |
| `POST` | `/identitytoolkit.googleapis.com/v1/projects/{project}/accounts:verifyIdToken` |

Password reset: `sendOobCode` with `requestType=PASSWORD_RESET` returns a lab `oobCode` (no email send). `resetPassword` consumes the code and sets `newPassword`.

`setCustomUserClaims` stores `customAttributes` / `claims` JSON on the user. `verifyIdToken` parses unsigned lab JWTs (`alg: none`) and returns `uid` / claims. Custom tokens and id tokens are **unsigned lab JWTs** (empty signature segment). Do not treat them as production credentials.

## Client configuration

```bash
export FIREBASE_AUTH_EMULATOR_HOST=127.0.0.1:4588
```

Many Firebase Admin / client SDKs honor `FIREBASE_AUTH_EMULATOR_HOST` and talk Identity Toolkit paths on that host. Send `targetProjectId` (or rely on the seeded default project) when the lab is multi-project-less.

Admin calls still need `Authorization: Bearer <token>`.

## Authz (admin)

- `firebaseauth.users.create|get|list|update|delete`

## Emulator limits

- Client Identity Toolkit methods do not require Bearer (emulator-shaped)
- Custom tokens and id tokens are unsigned lab JWTs (`alg: none`); not production credentials
- `sendOobCode` returns a lab `oobCode` only (no email delivery)
- No phone / OAuth / SAML / OIDC providers, MFA, blocking functions, or tenants

## Deferred depth

- Phone / OAuth / SAML / OIDC providers
- MFA, blocking functions, tenant management
- Signed JWTs / real Google public keys
- Session cookies with real cookies

## Verification / CLI smoke

```bash
go test ./internal/services/firebaseauth/ ./internal/server/ -run FirebaseAuth -count=1
curl -s -H "Content-Type: application/json" \
  -d '{"email":"a@example.com","password":"secret123","returnSecureToken":true}' \
  http://127.0.0.1:4588/identitytoolkit.googleapis.com/v1/accounts:signUp
```
