# Kano Navi Go read-only service

This module is the first migration slice. It is an independent Go service and
does not replace or modify the TypeScript server. It opens an existing Kano
SQLite snapshot read-only and exposes only the public health and revision
reads.

From the repository root, run:

```text
go run ./backend-go/cmd/kano-go -database data/database/kano.sqlite
```

The service listens on `:7658` by default, while the TypeScript service keeps
its existing `7657` port. Use `-listen 127.0.0.1:8765` to choose another
address. The database path is an explicit command-line option; there is no new
shared environment variable and `.env.example` remains unchanged.

The service must be pointed at an already initialized snapshot. It does not
create a file or directory, run migrations, seed data, enable WAL, or perform
any source, media, workflow, or model request. See
`docs/architecture/go-migration.md` for the migration boundary and deployment
notes.

Run the module checks from this directory:

```text
gofmt -w .
go test ./...
go vet ./...
```
