## Phase 1: Project Scaffold + DB Connection + Models

### Intent
Set up the Go project structure, database connection with auto-migration, and all 6 data model functions. This is the foundation everything else depends on.

### Files to create/modify

1. **`.env`** — `DB_URL` environment variable (pointing to a local postgres)
2. **`db.go`** — `initDB(dsn string) *sql.DB`:
   - Open connection with `lib/pq`
   - Ping to verify
   - Run `CREATE TABLE IF NOT EXISTS tasks (...)` migration
3. **`models.go`** — `Task` struct + 6 functions:
   - `GetAllTasks(db) ([]Task, error)`
   - `GetTaskByID(db, id) (Task, error)`
   - `CreateTask(db, title, note) (Task, error)`
   - `UpdateTask(db, id, title, note, status) (Task, error)`
   - `DeleteTask(db, id) error`
   - `CycleStatus(db, id) (Task, error)`
4. **`main.go`** — Entry point:
   - Load env, init DB, register a simple `/health` route, start server
5. **`go.mod`** — Initialize Go module, add `lib/pq` dependency

### Verification
- `go build ./...` compiles clean
- `go run .` starts without errors (DB must be available)
- Can hit `/health` endpoint
