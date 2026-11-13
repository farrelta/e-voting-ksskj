# e-voting-ksskj

E-voting project for the Selected Topic in Computer and Network class.

## Run the application (Windows)

### Requirements

- Docker Desktop installed and running with the Linux container engine enabled.
- Ports `3000`, `3001`, `5432`, and `6380` available.

### First-time setup

From the repository root, create the backend environment file:

```powershell
Copy-Item backend\.env.example backend\.env
```

Open `backend\.env` and replace `OTP_SECRET` with a random secret of at least
16 characters. For example, from PowerShell:

```powershell
notepad backend\.env
```

Then build and start PostgreSQL, Redis, the Go API, and the Next.js frontend:

```powershell
docker compose up --build
```

When the services finish starting, open <http://localhost:3000>. Keep this
terminal open to see service logs; stop the application with **Ctrl+C**. To run
it in the background instead, use `docker compose up --build -d`, then stop it
with `docker compose down`.

### Create a first admin account

The database starts empty, so add a verified admin account before signing in.
Open a second PowerShell window in the repository root and start the PostgreSQL
prompt:

```powershell
docker compose exec db psql -U evoting_app -d evoting_db
```

At the `evoting_db=#` prompt, insert an account, replacing the example email
and password with your own:

```sql
INSERT INTO evoting.users (email, password_hash, role, is_verified)
VALUES (
  'admin@example.test',
  crypt('replace-with-a-strong-password', gen_salt('bf', 12)),
  'admin',
  TRUE
);
```

Exit `psql` with `\q`. Sign in at <http://localhost:3000/login> using the email
and password you inserted. During local development, the backend prints the
one-time login code (OTP) in its logs. To find it when running in the
background, use:

```powershell
docker compose logs -f backend
```

Enter that six-digit code on the login page. The local database password in
Compose (`change-me`) and development OTP logging are for local development
only; configure secure credentials and OTP delivery before deploying anywhere
publicly.

### Existing database

If you already have a database volume from before the receipt-to-ballot link
was added, start only the database first:

```powershell
docker compose up -d db
```

Apply the migration from the repository root:

```powershell
Get-Content -Raw database\migrations\001_receipt_ballot_link.sql |
  docker compose exec -T db psql -U evoting_app -d evoting_db
```

Then start or rebuild the remaining services:

```powershell
docker compose up --build
```

Fresh databases get the latest schema directly from `database\schema.sql`.
Existing voter eligibility and receipt rows cannot be matched automatically:
the old schema did not store their association. Reconcile those records before
expecting existing registrations to appear in the voter dashboard.

### Troubleshooting

- If Docker reports that it cannot connect to the Docker API, start Docker
  Desktop and retry.
- If a port is already in use, stop the other service using it or change the
  published ports in `docker-compose.yaml`.
- View service status with `docker compose ps` and logs with
  `docker compose logs -f`.
- To stop the stack and remove its containers, run `docker compose down`.
  This preserves the database volume. **Do not run `docker compose down -v`
  unless you intend to delete your local database data.**

### Run frontend checks without Docker

With Node.js installed, the frontend can be type-checked and production-built
from PowerShell:

```powershell
Set-Location frontend
npm ci
npm run typecheck
npm run build
```

## Ballot cryptography

Ballot submission is intentionally disabled until an audited encryption and
zero-knowledge proof protocol, election public key, and proof verification
process are configured and wired into the Go API. The frontend does not
generate an invented proof, and the API rejects ballot submissions until a
verifier is installed. Candidate selections remain in the browser. Do not
enable ballot submission by substituting generic encryption or by sending a
plain candidate identifier to the API.
