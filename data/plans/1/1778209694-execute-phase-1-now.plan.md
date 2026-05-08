## Execute Phase 1: Scaffold + DB + Models

### Intent
Create all Phase 1 files, get them compiling, and verify the server starts.

### Files to create
1. **`.env`** — `DB_URL=postgres://user:postgres@localhost:5432/taskboard?sslmode=disable`
2. **`go.mod`** — `module taskboard` with `go 1.19`, `lib/pq` v1.10.9
3. **`db.go`** — `initDB(dsn)` → `*sql.DB`, auto-migrate `tasks` table
4. **`models.go`** — `Task` struct + 6 CRUD functions
5. **`main.go`** — load `.env`, init DB, `/health` route, listen `:8080`

### Steps
1. Check PostgreSQL is running, create DB if needed
2. Write all 5 files
3. `go mod tidy` to resolve deps
4. `go build ./...` to verify compilation
5. `go run .` quick start test, then stop
6. Commit

### Verification
- `go build ./...` exits 0
- Server starts and `/health` responds 200
