package main

import (
	"io/fs"
	"log/slog"
	"net/http"
)

// NewServer wires the mux, middleware, and dependencies into a single
// http.Handler.
func NewServer(logger *slog.Logger, config Config, store *DBStore, tmpl *Templates) http.Handler {
	mux := http.NewServeMux()
	addRoutes(mux, logger, config, store, tmpl)

	var handler http.Handler = mux
	handler = http.NewCrossOriginProtection().Handler(handler)
	handler = recovererMiddleware(handler)
	handler = loggingMiddleware(logger, handler)

	return handler
}

// addRoutes maps the entire surface of the app in one place.
func addRoutes(mux *http.ServeMux, logger *slog.Logger, config Config, store *DBStore, tmpl *Templates) {
	static, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /health", handleHealth())

	// everything except static files and the health check needs basic auth
	auth := func(h http.Handler) http.Handler { return basicAuthMiddleware(config, h) }

	mux.Handle("GET /{$}", auth(handleRoot()))
	mux.Handle("GET /day/{date}", auth(handleDayPage(store, tmpl)))
	mux.Handle("POST /day/{date}/note", auth(handleSaveDayNote(store)))
	mux.Handle("POST /day/{date}/activity/{id}/toggle", auth(handleToggleLog(store, tmpl)))
	mux.Handle("POST /day/{date}/activity/{id}", auth(handleSaveLog(store, tmpl)))

	mux.Handle("GET /calendar", auth(handleCalendarPage(store, tmpl)))
	mux.Handle("GET /stats", auth(handleStatsPage(store, tmpl)))
	mux.Handle("GET /export.csv", auth(handleExportCSV(store)))

	mux.Handle("GET /activities", auth(handleActivitiesPage(store, tmpl)))
	mux.Handle("GET /activities/new", auth(handleNewActivityPage(store, tmpl)))
	mux.Handle("POST /activities", auth(handleCreateActivity(store, tmpl)))
	mux.Handle("GET /activities/{id}/edit", auth(handleEditActivityPage(store, tmpl)))
	mux.Handle("POST /activities/{id}", auth(handleUpdateActivity(store, tmpl)))
	mux.Handle("POST /activities/{id}/archive", auth(handleArchiveActivity(store, true)))
	mux.Handle("POST /activities/{id}/unarchive", auth(handleArchiveActivity(store, false)))
	mux.Handle("POST /activities/{id}/move", auth(handleMoveActivity(store)))
}

func handleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}
}
