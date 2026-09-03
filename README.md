# GophKeeper

Client-server password manager: stores logins/passwords, texts, binary data and bank cards with per-user isolation. gRPC API, PostgreSQL storage, JWT auth.

## Layout

- `proto/gophkeeper/v1/` — protobuf sources (buf-managed, `buf lint` / `buf generate`)
- `internal/common/proto/gophkeeperv1/` — generated Go code (import path `github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1`), regenerate with `make generate`
- `cmd/server`, `cmd/client` — entry points
- `migrations/` — database migrations

## Common targets

- `make generate` — regenerate protobuf Go code (requires buf + `protoc-gen-go`/`protoc-gen-go-grpc`)
- `make lint` / `make test`
- `make build` — server and client binaries into `bin/` with version ldflags
- `make docker-build` — server image (`Dockerfile.server`)
- `make migrate-up` / `make migrate-down DSN=...` — database migrations
