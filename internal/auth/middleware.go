package auth

import (
	"net/http"
	"strings"
)

const unauthorizedBody = `{"error":{"code":"unauthorized","message":"a valid bearer token is required","details":{}}}`

func Middleware(hash string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
			if !found || !strings.EqualFold(scheme, "Bearer") || !Verify(hash, strings.TrimSpace(token)) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("WWW-Authenticate", "Bearer")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(unauthorizedBody))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
