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

Fresh databases get the latest schema from `database/schema.sql`. If an
existing database is already through migration `009`, run the upgrade below
with only `010_drop_voting_mode_and_pilot_only.sql` in the migration list.
For older volumes, start the list at the first migration you still need.

```bash
(
  set -e
  docker compose stop frontend backend
  docker compose up -d --wait db
  backup="$(mktemp "${TMPDIR:-/tmp}/evoting-before-upgrade.XXXXXX")"
  docker compose exec -T db pg_dump -U evoting_app -Fc evoting_db > "$backup"
  printf 'Database backup saved to %s\n' "$backup"
  for migration in \
    002_candidate_position_and_election_keys.sql \
    002_election_crypto_and_results.sql \
    003_consolidate_election_keys.sql \
    004_anonymous_voting.sql \
    005_anonymous_issuer_keys.sql \
    006_anonymous_ballot_credentials.sql \
    007_default_anonymous_voting.sql \
    008_drop_legacy_ballot_tables.sql \
    009_drop_created_at.sql \
    010_drop_voting_mode_and_pilot_only.sql
  do
    docker compose exec -T db psql -v ON_ERROR_STOP=1 -U evoting_app -d evoting_db \
      < "database/migrations/$migration"
  done
  docker compose up --build -d
)
```

Keep the database dump in secure storage. Run both historical `002` files in
the order shown, even if one has already been applied. Migration `003` uses
`election_keys` as the canonical key table. If it reports legacy data, resolve
the discrepancy with the database owner before proceeding. The cleanup
transaction rolls back when that check fails.

Migrations `004` through `006` add anonymous voting storage and `007` changes
the default for new elections. Migration `008` drops `receipts` and
`encrypted_ballots`, removes `receipt_id` and `has_voted` from voter
eligibility, and closes all receipt elections. The old ballots and receipt
codes are deleted. Election metadata, eligible voter lists, and results
already saved in `election_results` are retained. Complete and save any
outstanding legacy tally with the old version before applying `008` if
you need those results. The historical `001` migration is superseded by
this cleanup and is not needed in the upgrade sequence.

Migration `009` drops `created_at` from users, elections, and election keys.
The voting schedule and the timestamp on the aggregate tally still apply.

Migration `010` drops `elections.voting_mode` and
`anonymous_issuer_keys.pilot_only`. It removes credential-only test keys and
archives elections without an issuer. Existing anonymous ballot elections
keep their issuer keys, ballots, and trackers.

### Back up the tally key

Create elections from the admin page on HTTPS or localhost. Choose a backup
passphrase and download the encrypted tally key file before leaving the page.
Keep a copy of the file and store the passphrase separately. After voting ends,
open **Tally votes** on the election card and provide both. For elections made
before this UI was added, use the existing tally API with the original HE key
if you saved it. Its database fingerprint cannot recover a lost key. Elections
that predate key provisioning need their original keys restored. For a
disposable election with no voters or ballots, create a replacement election
and save its new backup.

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

## Anonymous elections

New elections start as anonymous drafts. Add candidates and eligible voters,
save the encrypted tally key backup, then open registration. After voting
opens, eligible voters can cast an encrypted ballot and save the ballot hash.
The public verification page accepts that hash to check whether the ballot
was recorded. Legacy receipt codes expired when the old tables were removed.

The anonymous prototype is enabled by default in development. Set
`ANONYMOUS_BALLOT_PILOT=false` to disable it. It remains unavailable outside
development. See [anonymous voting](database/anonymous-voting.md) for the
local workflow and the limits of the prototype.

## Participants and the automatic schedule

**Participants.** In **Admin -> Eligible voters**, paste the participants' email
addresses (one per line, or separated by commas; names and other spreadsheet
columns are ignored) and press **Add participants**. Only addresses on
`ALLOWED_EMAIL_DOMAIN` (default `ugm.ac.id`, subdomains such as `mail.ugm.ac.id`
included) are accepted; anything else is reported back and not added. People
without an account get one automatically. These accounts have no password; the
intended login for them is the campus SSO.

**Schedule.** Opening registration publishes an election. From then on the backend
opens voting at the voting start time and closes it at the voting end time
(checked every 5 seconds). A ballot is also rejected outside the voting window even
if the status says otherwise. Draft elections are never opened automatically.

**Database tests.** The integration tests run only when pointed at a database that
has `database/schema.sql` loaded:

```powershell
$env:TEST_DATABASE_URL = "postgres://evoting_app:change-me@localhost:5432/evoting_db"
cd backend; go test ./...
```

They create and remove their own rows, but use a development database, not real data.

## Voter QR sign-in

Eligible voters sign in by scanning a QR code instead of typing a password.

1. In **Admin → Eligible voters**, add the voter's email, then press **Login QR**
   next to their name.
2. The voter scans the QR with their phone. The link is single-use, expires after
   15 minutes, and is cancelled if the admin creates a new QR for that voter.
3. Scanning only identifies the voter. They must still enter the six-digit one-time
   code to finish signing in.

A phone cannot open `localhost`. Before starting, set `PUBLIC_APP_URL` to an address
the phone can reach (this computer's LAN IP or a domain), for example:

```powershell
$env:PUBLIC_APP_URL = "http://192.168.1.20:3000"
docker compose up --build
```

The one-time code is emailed through the same SMTP settings as the password login
(in development without SMTP it is printed in the backend log instead).
