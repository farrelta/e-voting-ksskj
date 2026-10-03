# Go backend

The Docker Compose `backend` service builds this directory.

Create the local configuration file before starting the stack:

```bash
cp backend/.env.example backend/.env
```

Set a random `OTP_SECRET` with at least 16 characters in that file, then start the services from the repository root:

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