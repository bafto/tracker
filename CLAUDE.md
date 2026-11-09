# CLAUDE.md

Small private single-user web app to track daily/weekly activities. **See `PLAN.md`** for the design and the TODO checklist — tick items there as work is completed.

## ⚠ In production
The app is deployed and holds real data; every push to `master` ships a new image. From now on:
- **Never edit existing migrations.** Schema changes go in a new numbered `sql/migrations/00N_*.up.sql`, must preserve existing data, and must work on the live DB (SQLite `ALTER TABLE` is limited — use the create/copy/drop/rename pattern when needed).
- **Avoid breaking changes** to config keys, URLs and stored data formats (e.g. `YYYY-MM-DD` days, schedule semantics); if unavoidable, migrate old data/config and call it out.
- Do riskier work on a branch, not directly on `master`.

## Commands
- Build: `go build ./...`
- Run: `PASSWORD=secret go run .` (reads optional `tracker_config.yaml` from the cwd, see `tracker_config.example.yaml`; env vars matching the keys, e.g. `PASSWORD`, override)
- Vet: `go vet ./...`
- Regenerate `db/` after editing `sql/migrations/*.sql` or `sql/queries/*.sql`: `sqlc generate`
- Docker: the image has no env config; mount a config (based on `tracker_config.docker.yaml`) and a data dir:
  `docker run -p 8080:8080 -v $PWD/tracker_config.yaml:/app/tracker_config.yaml:ro -v $PWD/data:/data tracker`
- Tests: deliberately minimal (private app, bugs are reported manually). Only add tests if absolutely critical.

## Architecture
Plain `net/http` + `html/template` + htmx, following the `../SpotiMyServer` style.
- `routes.go` — `NewServer` (middleware: logging → recoverer → CrossOriginProtection) and `addRoutes`, the single map of all routes. Everything except `/static/` and `/health` is wrapped in `basicAuthMiddleware`.
- `handlers_*.go` — maker-function handlers `handleX(deps) http.HandlerFunc`. Forms work without JS; htmx requests (`HX-Request`) get the `day_row` fragment instead of a redirect.
- `templates.go` — embeds `templates/*.gohtml` and `static/`. Each page = `layout.gohtml` + its own file (which may `define` fragments); `render` / `renderFragment`.
- `schedule.go` — pure domain logic: days as UTC-midnight `time.Time`, today in `time.Local` (set from the `timezone` config key in `run()`, zone db embedded via `time/tzdata`); schedules (weekday bitmask Mon=bit0, or N/week), completion rate and streak rules (documented on `ComputeStats`).
- `store.go` (`DBStore`, `withinTransaction`, `getQueries`), `activity_store.go`, `log_store.go` — DB access, mapping sqlc rows to domain types.
- Schedules are versioned (`activity_schedule.effective_from`); the initial one is effective from `1970-01-01`. Editing a schedule adds a version effective today, unless nothing was logged before today (then history is replaced).
- Auth: HTTP Basic Auth on every request (no sessions/cookies). Password (and optional username) from config, compared as sha256 hashes in constant time.
