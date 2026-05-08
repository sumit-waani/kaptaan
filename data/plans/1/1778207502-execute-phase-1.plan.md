## Phase 1: Project Scaffold + DB Connection + Models

### Intent
Execute the Phase 1 plan already on disk: set up Go project structure, database connection with auto-migration, and all 6 data model functions.

### Files to create

1. **`.env`** — `DB_URL` for local postgres
2. **`go.mod`** — Go module (go 1.19, constrained per sandbox)
3. **`db.go`** — `initDB(dsn string) *sql.DB` with `lib/pq`, CREATE TABLE migration
4. **`models.go`** — `Task` struct + 6 functions (GetAllTasks, GetTaskByID, CreateTask, UpdateTask, DeleteTask, CycleStatus)
5. **`main.go`** — Entry point: load env, init DB, register `/health` route, start server

### Verification
- `go build ./...` compiles clean
- `go run .` starts without errors (Postgres available)
- Can curl `/health`
