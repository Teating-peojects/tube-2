.PHONY: up down test vet tidy check test-integration test-db promote migrate-up migrate-down db-check dictionary

up:    ; docker compose up --build
down:  ; docker compose down
test:  ; go test ./...
vet:   ; go vet ./...
check: ; go vet ./... && go test ./...
# Resolve go.sum without installing Go locally:
tidy:  ; docker run --rm -v "$$PWD":/src -w /src golang:1.22-alpine go mod tidy

# --- database (psql-based; no Docker needed). Run migrations as redxtube_migrator, never as a superuser. ---
# DATABASE_URL=postgres://redxtube_migrator:...@host:5432/redxtube
migrate-up:   ; ../db/scripts/migrate.sh up
migrate-down: ; ../db/scripts/migrate.sh down 1
db-check:     ; python3 ../db/tools/dbcheck.py
dictionary:   ; python3 ../db/tools/gen_dictionary.py

# --- tests ---
# Builds redxtube_test (roles + migrations + categories). ADMIN_URL is a superuser DSN, e.g.
#   make test-db ADMIN_URL=postgres://postgres:secret@127.0.0.1:5432/postgres
test-db:      ; ADMIN_URL=$(ADMIN_URL) ../db/scripts/test-db.sh
# needs TEST_ADMIN_DATABASE_URL, TEST_DATABASE_URL, TEST_REDIS_URL (test-db prints them)
test-integration: ; go test -count=1 -race ./...

promote:      ; ../security/scripts/promote.sh $(EMAIL) $(ROLE)   # make promote EMAIL=me@x.com ROLE=ADMIN
