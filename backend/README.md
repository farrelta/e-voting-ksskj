# Go backend

The Docker Compose `backend` service builds this directory.

Create the local configuration file before starting the stack:

```bash
cp backend/.env.example backend/.env
```

Set a random `OTP_SECRET` with at least 16 characters in that file. In development, OTPs are logged only when SMTP is not configured. To email OTPs locally, set `SMTP_HOST`, `SMTP_PORT`, `SMTP_FROM`, and (when required by your provider) `SMTP_USER` and `SMTP_PASSWORD`. SMTP delivery requires STARTTLS; port `587` is the usual choice. Outside development, SMTP host and sender address are required.

Then start the services from the repository root:

```bash
docker compose up --build
```

## Google sign-in (OAuth 2.0 / OpenID Connect)

Google sign-in is optional and is disabled while `GOOGLE_CLIENT_ID` is empty.
When disabled, the `/api/auth/google` routes are not registered at all.

An account can only sign in with Google if an administrator has already created
it. Google sign-in replaces the password step only: the email one-time code is
still required afterwards, so the flow stays two-factor.

### 1. Create the OAuth client

1. Open <https://console.cloud.google.com/apis/credentials> and pick (or create) a project.
2. Configure the OAuth consent screen. Set the user type to **Internal** for a
   Google Workspace domain so only that organisation can consent.
3. Create credentials → **OAuth client ID** → application type **Web application**.
4. Under *Authorized redirect URIs* add the URL of the backend callback. It must
   match `GOOGLE_REDIRECT_URL` byte for byte — scheme, host, port, path and
   trailing slash all count:

   ```
   http://localhost:3000/api/auth/google/callback
   ```

   Port `3000` is the Next.js frontend, which proxies `/api/*` to the backend.
   For a deployment, use `https://your-host/api/auth/google/callback`.

5. Copy the client ID and client secret into `backend/.env`:

   ```
   GOOGLE_CLIENT_ID=....apps.googleusercontent.com
   GOOGLE_CLIENT_SECRET=...
   GOOGLE_REDIRECT_URL=http://localhost:3000/api/auth/google/callback
   ```

The backend refuses to start if only one of the credentials is set, or if the
client ID is set without a redirect URL.

### 2. How the flow works

```
GET /api/auth/google          -> 302 to Google, remembering a single-use state + nonce
GET /api/auth/google/callback -> verifies state, exchanges the code server to server,
                                 verifies the ID token, links the identity, sends an OTP,
                                 then redirects to /login?step=otp
POST /api/login/verify-otp    -> unchanged, completes sign-in
```

Notes on the checks the callback performs:

- `state` is consumed atomically with `GETDEL`, so a callback can never be replayed.
- The ID token signature is verified against Google's JWKS (cached, refreshed on
  unknown `kid`). Only `RS256` is accepted.
- `iss`, `aud`, `exp` and `nonce` are all verified.
- `email_verified` must be true, and `hd` must equal `ALLOWED_EMAIL_DOMAIN`.
  Google documents that the email domain cannot be trusted for organisation
  membership, so the signed `hd` claim is the gate.
- The linked identity is keyed on `sub`, never on email, because a Google account
  email can change over time.
- An unknown email is rejected. Accounts are only ever created by an administrator.

Run the Go checks locally:

```bash
cd backend
go test ./...
go test -race ./...
go vet ./...
```
