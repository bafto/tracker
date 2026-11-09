# Tracker — Plan

> **Status: in production.** DB changes only via new migrations (never edit existing ones), avoid breaking changes on `master` — see `CLAUDE.md`.

## Context
Empty repo (`go.mod` only, module `github.com/bafto/tracker`, go 1.27.3). Goal: a paper-simple, single-user web app to define recurring activities (cleaning, workouts, writing, uni…), log what was done each day (with optional amount + note), and review history (calendar grid, totals, streaks, completion rate). Self-hosted via Docker image built by a GH Action, same as `../FindFavouriteSong`. Backend code style follows `../SpotiMyServer`.

## Decisions (agreed)
- **Single user**, HTTP Basic Auth on every request (password + optional username from config). No sessions/cookies.
- **Frontend:** Go `html/template` + **htmx** (vendored `static/htmx.min.js`, pinned 2.x), handwritten CSS. Templates + static files `//go:embed`ed into the binary. No build step. Responsive / usable on phone browsers; no PWA/offline.
- **Log entry:** one per (activity, day) = "done". Optional `amount` (unit defined per activity: min/km/pages/none) and optional note.
- **Schedule:** per activity, either fixed weekdays (bitmask; all 7 = daily) or N times/week. **Versioned** with `effective_from` date so history is judged by the schedule in force then.
- **Archive** instead of delete (hidden from Today, kept in history/stats).
- **Backfill** any past day (no future days).
- **Overview:** calendar grid (activities × days, month view), totals per activity, streaks + completion rate. (No year heatmap.)
- **Extras:** CSV export, per-activity color + manual ordering, one journal note per day.
- **Weeks:** Monday start; "today" computed in `Europe/Berlin` (TZ env, as in FFS Dockerfile).

## Data model — `sql/migrations/001_initialize_schema.up.sql`
```sql
activity(id INTEGER PK, name TEXT NOT NULL, unit TEXT NULL, color TEXT NOT NULL,
         sort_order INTEGER NOT NULL, archived_at TEXT NULL, created_at TEXT NOT NULL)
activity_schedule(id INTEGER PK, activity_id FK->activity ON DELETE CASCADE,
         effective_from TEXT NOT NULL,           -- 'YYYY-MM-DD'
         kind TEXT NOT NULL CHECK(kind IN ('weekdays','weekly')),
         weekday_mask INTEGER NOT NULL DEFAULT 0, -- bit0=Mon … bit6=Sun
         times_per_week INTEGER NOT NULL DEFAULT 0,
         UNIQUE(activity_id, effective_from))
log_entry(activity_id FK, day TEXT NOT NULL, amount REAL NULL, note TEXT NULL,
         created_at TEXT, updated_at TEXT, PRIMARY KEY(activity_id, day))
day_note(day TEXT PK, note TEXT NOT NULL)
```
Schedule in effect on day D = row with max `effective_from <= D`. Editing a schedule inserts a new row effective today (upsert if one already exists for today). First schedule row is effective from the activity's creation date (or earliest log date, so backfilled days are covered).

## Domain logic — `schedule.go` (pure Go, unit tested)
- `isDue(schedule, day)` for weekdays kind; weekly kind uses schedule in effect on that week's Monday.
- **Completion rate** over a range: weekdays → done-on-due-days / due-days (bonus days don't inflate); weekly → Σ min(count, N) / Σ N over full weeks (current partial week shown separately as "x/N this week").
- **Streaks:** weekdays → consecutive due days done (non-due days skipped; today not yet done doesn't break it); weekly → consecutive weeks meeting N (current week doesn't break it). Current + longest.
- **Totals:** count of done days and Σ amount per activity in range.
Stats loaded with a few range queries and computed in Go (data is tiny).

## Pages & routes — `routes.go` (`addRoutes`, single place, SpotiMyServer style)
| Route | Purpose |
|---|---|
| `GET /` → redirect `/day/{today}` | |
| `GET /day/{date}` | **Today page**: prev/next/"today" nav, "Due" list (weekday-due + weekly with `x/N this week`), collapsed "Not due today" list, day journal note |
| `POST /day/{date}/activity/{id}/toggle` | htmx: toggle done, returns the row fragment |
| `POST /day/{date}/activity/{id}` | htmx: save amount/note (row expands to small inline form) |
| `POST /day/{date}/note` | save/clear journal note |
| `GET /calendar?month=YYYY-MM` | grid: rows = activities (color dot), cols = days; ✓ / amount in cell, due-but-missed shaded, journal-note marker row; cells link to `/day/{date}`; horizontally scrollable on phones |
| `GET /stats?range=week\|month\|year\|all` (+ `from`/`to`) | per activity: done count, Σ amount+unit, completion %, current/longest streak |
| `GET /activities` + new/edit forms, `POST /activities`, `POST /activities/{id}`, `POST /activities/{id}/archive\|unarchive`, `POST /activities/{id}/move?dir=up\|down` | manage activities: name, unit, color, schedule kind + mask / N |
| `GET /export.csv` | `date,activity,amount,unit,note` + day notes |
| `GET /health`, `GET /static/…` | |

All except health/static wrapped in `basicAuthMiddleware` (401 + `WWW-Authenticate` challenge).

## Files to create (mirroring SpotiMyServer layout)
- `main.go` — `run(ctx, stderr, loadConfig)` with signal context, tint logger, graceful shutdown (copy structure of `SpotiMyServer/main.go`).
- `config.go` — viper, `tracker_config.yaml` optional + env override; keys `host`, `port` 8080, `db_conn`, `timezone` (default Europe/Berlin), `username` (optional), `password` (required).
- `db.go`, `migrations.go`, `store.go` — copy SpotiMyServer's `setupDB`, `migrateDB` (embedded `sql/migrations`), `DBStore` with `withinTransaction`/`getQueries`.
- `activity_store.go`, `log_store.go` — `DBStore` methods mapping sqlc rows → domain types.
- `middleware.go` — `loggingMiddleware`, `recovererMiddleware` (copied), `authMiddleware`.
- `templates.go` — embed + parse `templates/*.gohtml` (base layout + page + fragment templates), `render(w, r, name, data)` helper.
- `handlers_day.go`, `handlers_activities.go`, `handlers_overview.go` (calendar, stats, export), `handlers_auth.go` — maker functions `handleX(deps) http.HandlerFunc`.
- `schedule.go`, `schedule_test.go`.
- `templates/` (`layout`, `day`, `day_row` fragment, `calendar`, `stats`, `activities`, `activity_form`), `static/style.css` (CSS vars, light/dark via `prefers-color-scheme`, mobile-first, big tap targets), `static/htmx.min.js`.
- `sql/queries/{activity,log}_queries.sql`, `sqlc.yaml` (same as SpotiMyServer's), generated `db/`.
- `Dockerfile` — FFS two-stage (golang:alpine + gcc for CGO sqlite → alpine + tzdata, `TZ=Europe/Berlin`); no template COPY needed (embedded); DB path via `DB_CONN` pointing into a mounted volume (`/data/tracker.db`).
- `entrypoint.sh`, `.github/workflows/build_and_push_image.yaml` — copy of FFS workflow, tag `bafto/hetzner:tracker`, trigger on push to `master`.
- `CLAUDE.md`, `.gitignore` (`*.db*`, `tracker_config.yaml`), `tracker_config.example.yaml`.

## Verification
1. `sqlc generate && go vet ./... && go build ./...`
2. `go test ./...` — `schedule_test.go` (due/completion/streak edge cases: schedule changes mid-week, today undone, weekly partial week) and `e2e_test.go` modeled on SpotiMyServer's `startTestServer` (temp DB): auth required, create activity, toggle today/backfill, amount+note, archive, calendar/stats render, CSV content.
3. Run locally: `PASSWORD=test go run .`, click through in a browser at desktop and phone width (use the `run` skill).
4. `docker build -t tracker . && docker run -e PASSWORD=x -v $PWD/data:/data -p 8080:8080 tracker`, check data persists across restarts.

## TODO

Tick items (`- [x]`) as they are completed so work can be resumed across sessions.

### 1. Scaffolding
- [x] `main.go` (run(), signal ctx, tint logger, graceful shutdown)
- [x] `config.go` (viper, `tracker_config.yaml` + env, bcrypt password)
- [x] `db.go`, `migrations.go`, `store.go` (DBStore, withinTransaction, getQueries)
- [x] `middleware.go` (logging, recoverer)
- [x] `sqlc.yaml`, `.gitignore`, `tracker_config.example.yaml`

### 2. Schema + queries
- [x] `sql/migrations/001_initialize_schema.up.sql`
- [x] `sql/queries/*.sql`, `sqlc generate`

### 3. Domain logic
- [x] `schedule.go` (due, completion rate, streaks, totals)

### 4. Auth
- [x] HTTP Basic Auth middleware (replaced the earlier session/login-page design)

### 5. Activities
- [x] list/new/edit forms (name, unit, color, schedule kind, weekday mask / N per week)
- [x] schedule versioning (new row effective today on change)
- [x] archive / unarchive
- [x] move up/down ordering

### 6. Day page
- [x] due / not-due sections, `x/N this week` for weekly
- [x] htmx toggle done
- [x] amount + note inline form
- [x] day journal note
- [x] prev/next/today navigation, no future days

### 7. Calendar
- [x] month grid activities x days, due-but-missed shading, note markers, links to day

### 8. Stats
- [x] ranges (week/month/year/all/custom): count, sum amount, completion %, current/longest streak

### 9. CSV export
- [x] `/export.csv`

### 10. Styling
- [x] `static/style.css` responsive, light/dark
- [x] vendored `static/htmx.min.js`

### 11. Tests
- [x] Skipped by decision: private app, bugs get reported manually. Only add tests if absolutely critical.

### 12. Deployment
- [x] All config via mounted `tracker_config.yaml` (no env in Dockerfile); `timezone` config key
- [x] `Dockerfile`, `entrypoint.sh`
- [x] `.github/workflows/build_and_push_image.yaml` (tag `bafto/hetzner:tracker`)

### 13. Docs
- [x] `CLAUDE.md` (pointer to this file)
