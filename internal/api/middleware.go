package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

const RequestIDHeader = "X-Request-Id"

type contextKey string

const requestIDKey contextKey = "request_id"

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func recoveryMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered",
						slog.Any("panic", rec),
						slog.String("path", r.URL.Path),
						slog.String("request_id", requestIDFrom(r.Context())))
					writeJSON(w, http.StatusInternalServerError, map[string]any{
						"error": map[string]any{
							"code":    CodeInternalError,
							"message": "an internal error occurred",
							"details": map[string]any{"request_id": requestIDFrom(r.Context())},
						},
					})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func loggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			logger.Info("request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("request_id", requestIDFrom(r.Context())),
				slog.Duration("duration", time.Since(start)))
		})
	}
}
