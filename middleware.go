package main

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
	"uuid"

)

type (
	loggerKey    struct{}
	requestIdKey struct{}
)

func getLogger(ctx context.Context) *slog.Logger {
	logger, _ := ctx.Value(loggerKey{}).(*slog.Logger)
	return cmp.Or(logger, slog.Default())
}

func withLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

func getRequestID(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(requestIdKey{}).(uuid.UUID)
	return id
}

func withRequestID(ctx context.Context, requestID uuid.UUID) context.Context {
	return context.WithValue(ctx, requestIdKey{}, requestID)
}

// injects requestID and logger into request context
//
// Responses: none of its own; never writes to w and always calls next.
func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := uuid.NewV7()
		logger := logger.With("method", r.Method, "path", r.URL.Path, "request-id", requestID.String())
		r = r.WithContext(withLogger(
			withRequestID(r.Context(), requestID),
			logger,
		))
		logger.Info("got request")
		next.ServeHTTP(w, r)
		logger.Info("request finished", "duration", time.Since(start))
	})
}

// recovererMiddleware turns a panic in a handler into a 500 instead of
// crashing the server.
//
// Responses: 500 Internal Server Error (recovered panic); otherwise passes
// through whatever next writes.
func recovererMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := getLogger(r.Context())
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error(fmt.Sprintf("panic recovered: %s", string(debug.Stack())), "err", rec)

				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

