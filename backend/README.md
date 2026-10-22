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

Run the Go checks locally:

```bash
cd backend
go test ./...
go test -race ./...
go vet ./...
```
