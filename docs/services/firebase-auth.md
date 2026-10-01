# Firebase Auth (Identity Toolkit)

Identity Toolkit REST for email/password auth, password-reset OOB codes, admin user CRUD, custom claims, and HS256 id-token verify.

## Status

Implemented: signUp / signInWithPassword / lookup / update / delete, sendOobCode / resetPassword, admin user CRUD with pagination, setCustomUserClaims, verifyIdToken, custom-token exchange.

## Wire protocol

Client methods (emulator-shaped; identifier lookup still needs admin Bearer):

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

Client `accounts:update` and `accounts:delete` require a valid lab `idToken`. When `localId` is also sent, it must match the token `user_id`/`sub`. Missing `idToken` returns `401` `MISSING_ID_TOKEN`; invalid or mismatched token returns `400` `INVALID_ID_TOKEN`.

Client `accounts:lookup` with only `idToken` is the public getAccountInfo path: the id token must verify as a process HS256 Identity Toolkit token (same as update/delete); that uid is returned. Firebase client "Get user data" sends `idToken` and nothing else. `email[]`, `localId[]`, `phoneNumber[]`, and `federatedUserId[]` are admin identifier queries. They need Bearer plus `firebaseauth.users.get` or `firebaseauth.users.list`, or root. No admin principal returns `401` `MISSING_ID_TOKEN`. A Bearer principal without those permissions returns `403`. Identifier queries do not return `userRecord` on deny.

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
| `POST` | `/identitytoolkit.googleapis.com/v2/projects/{project}/tenants` (`?tenantId=`) |
| `GET` | `/identitytoolkit.googleapis.com/v2/projects/{project}/tenants` |
| `GET` | `/identitytoolkit.googleapis.com/v2/projects/{project}/tenants/{tenant}` |
| `PATCH` | `/identitytoolkit.googleapis.com/v2/projects/{project}/tenants/{tenant}` |

Password reset: `sendOobCode` with `requestType=PASSWORD_RESET` returns a lab `oobCode` (no email send). `resetPassword` consumes the code and sets `newPassword`.

`setCustomUserClaims` stores `customAttributes` / `claims` JSON on the user. Id tokens are HS256-signed with the process signing key (`alg: HS256`). Reserved claims (`user_id`, `sub`, `exp`, `iss`, `aud`, and related) are set by the mint path and cannot be overwritten by custom attributes. `verifyIdToken`, public lookup, and Bearer acceptance require a valid signature; unsigned tokens (`alg: none`) are rejected. Admin `createCustomToken` mints HS256 custom tokens; `:signInWithCustomToken` verifies that signature before minting an id token. Custom tokens are not accepted as control-plane Bearers. Email-shaped Toolkit localIds authenticate as `user:{uid}` so they do not match `serviceAccount:` IAM bindings.

v2 tenant CRUD stores `allowPasswordSignup`. `accounts:signUp` with `tenantId` of a locked tenant (`allowPasswordSignup=false`) returns `admin-restricted-operation`. Open tenants accept email/password sign-up.

## Client configuration

```bash
export FIREBASE_AUTH_EMULATOR_HOST=127.0.0.1:4588
```

Many Firebase Admin / client SDKs honor `FIREBASE_AUTH_EMULATOR_HOST` and talk Identity Toolkit paths on that host. Send `targetProjectId` (or rely on the seeded default project) when the lab is multi-project-less.

Admin calls still need `Authorization: Bearer <token>`.

## Authz (admin)

- `firebaseauth.users.create|get|list|update|delete`
- `identitytoolkit.tenants.create|get|list|update`

## Emulator limits

- Client Identity Toolkit methods skip middleware Bearer (emulator-shaped)
- Client `accounts:lookup` with `idToken` only is public self-lookup; `email[]` / `localId[]` / phone / federated need admin Bearer as above
- Client `accounts:update` / `accounts:delete` require lab `idToken` matching `localId` when provided; admin project CRUD remains Bearer-only
- Id tokens and custom tokens use process HS256 keys (not Google public keys); custom-token exchange requires a verified custom token
- `sendOobCode` returns an `oobCode` only (no email delivery)
- No phone / OAuth / SAML / OIDC providers, MFA, or blocking functions

## Deferred depth

- Phone / OAuth / SAML / OIDC providers
- MFA, blocking functions
- Real Google public-key JWT verify
- Session cookies with real cookies

## Verification / CLI smoke

```bash
go test ./internal/services/firebaseauth/ ./internal/server/ -run FirebaseAuth -count=1
curl -s -H "Content-Type: application/json" \
  -d '{"email":"a@example.com","password":"secret123","returnSecureToken":true}' \
  http://127.0.0.1:4588/identitytoolkit.googleapis.com/v1/accounts:signUp
```
