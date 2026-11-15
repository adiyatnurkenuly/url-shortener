# URL Shortener API

A small URL shortening service written in Go. It creates short links, redirects visitors, stores links in PostgreSQL, and tracks the number of redirects.

## Tech Stack

- Go (standard `net/http`)
- PostgreSQL (`pgx` via `database/sql`)
- Docker and Docker Compose
- Go tests with `httptest` and `sqlmock`
- GitHub Actions (formatting, tests, vet, build)

## Features

- Generate short, random URL-safe codes
- Persist links in PostgreSQL
- Redirect with HTTP 302
- Count redirects atomically in PostgreSQL
- View link statistics
- Delete short links
- Validate input URLs and JSON requests

## API

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Check server availability |
| POST | `/links` | Create a short link |
| GET | `/{code}` | Redirect to the original URL |
| GET | `/links/{code}/stats` | View click count and creation time |
| DELETE | `/links/{code}` | Delete a short link |

### Example: create a link

Request:

```http
POST /links
Content-Type: application/json
```

```json
{"url": "https://example.com"}
```

Response (201 Created; generated code varies):

```json
{
  "code": "AbC123xy",
  "original_url": "https://example.com",
  "short_url": "http://localhost:8081/AbC123xy"
}
```

Visiting the short URL responds with HTTP 302 and increases the click counter.

## Run locally with Docker

Requirements: Docker Desktop / Docker Engine with Docker Compose.

1. Clone the repository and enter the project directory:

   ```powershell
   git clone https://github.com/adiyatnurkenuly/url-shortener.git
   cd url-shortener
   ```

2. Create your local environment file:

   ```powershell
   Copy-Item .env.example .env
   ```

3. Replace the placeholder value of `POSTGRES_PASSWORD` in `.env` with a unique, randomly generated password. Do not commit `.env`.

4. Start the app and PostgreSQL:

   ```powershell
   docker compose up --build -d
   docker compose ps
   ```

5. Check the service with `GET http://localhost:8081/health` (for example, in Postman).

The API is bound to `127.0.0.1:8081` on your computer. The container's PostgreSQL is available locally on port `5434` if needed for development. The schema in `migrations/001_init.sql` is applied only when a new PostgreSQL data volume is initialized.

To stop the services without deleting stored links:

```powershell
docker compose down
```

## Postman

Import `postman/url-shortener.postman_collection.json` into Postman. It has a `baseUrl` collection variable set to `http://localhost:8081` and saves the generated short code after `POST /links`.

Run requests in order: Health → Create Link → Redirect → Get Stats → Delete Link → Verify Deleted. The Redirect request is configured not to follow redirects automatically, so Postman can display the HTTP 302 response.

## Run tests

Go is required to run tests outside Docker.

```powershell
go test -v ./...
go vet ./...
```

Unit tests use `sqlmock`; no running PostgreSQL is required for them. For end-to-end verification, run Docker Compose and use Postman.

## Continuous Integration

The GitHub Actions workflow (`.github/workflows/ci.yml`) runs formatting checks, tests, `go vet`, and build checks on pushes and pull requests to `main`.

## Security Notes

This is a learning and portfolio project intended for local use. Creating and deleting links requires no authentication, and the service does not have rate limiting or protections against abuse. Do not expose it to the public internet without adding appropriate access control, safeguards, and request limits. Do not commit passwords or `.env` files.

## Author

[adiyatnurkenuly](https://github.com/adiyatnurkenuly)
