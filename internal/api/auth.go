package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/getorcal/orcal/internal/auth"
)

const unauthorizedBody = `{"error":{"code":"unauthorized","message":"a valid bearer token is required","details":{}}}`

const principalKey contextKey = "principal"

func principalFrom(ctx context.Context) *auth.Token {
	token, _ := ctx.Value(principalKey).(*auth.Token)
	return token
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", "Bearer")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(unauthorizedBody))
}

func bearerToken(r *http.Request) string {
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := s.tokens.Authenticate(r.Context(), bearerToken(r))
		if err != nil {
			if !errors.Is(err, auth.ErrUnauthorized) {
				s.writeError(w, r, err)
				return
			}
			writeUnauthorized(w)
			return
		}
		annotate(r.Context(), func(a *annotation) {
			a.actorTokenID = token.ID
			a.actorName = token.Name
		})
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, token)))
	})
}

func (s *Server) requireScope(want auth.Scope, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal := principalFrom(r.Context())
		if principal == nil || !principal.Scopes.Has(want) {
			annotate(r.Context(), func(a *annotation) {
				a.details = map[string]any{"required_scope": string(want)}
			})
			s.writeError(w, r, fmt.Errorf("%w: this token does not hold %s", ErrForbidden, want))
			return
		}
		next(w, r)
	}
}
