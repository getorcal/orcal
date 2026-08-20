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
		header := r.Header.Get("Authorization")
		raw := bearerToken(r)
		token, err := s.tokens.Authenticate(r.Context(), raw)
		if err != nil {
			if !errors.Is(err, auth.ErrUnauthorized) {
				s.writeError(w, r, err)
				return
			}
			annotate(r.Context(), func(a *annotation) {
				a.details = deniedAuthDetails(header, raw, token, err)
			})
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

// deniedAuthDetails names the reason authenticate rejected a request, one of missing, malformed,
// unknown, expired, or revoked. The credential's prefix is included only when it resolved to a
// known token record — resolved is non-nil only for the expired and revoked cases — so an
// attacker-supplied bearer string is never echoed back into the audit log.
func deniedAuthDetails(header, raw string, resolved *auth.Token, err error) map[string]any {
	reason := "unknown"
	switch {
	case header == "":
		reason = "missing"
	case raw == "":
		reason = "malformed"
	case errors.Is(err, auth.ErrTokenRevoked):
		reason = "revoked"
	case errors.Is(err, auth.ErrTokenExpired):
		reason = "expired"
	}
	details := map[string]any{"reason": reason}
	if resolved != nil {
		details["token_prefix"] = resolved.Prefix
	}
	return details
}

// missingScopeError carries the scope a 403 was refused for, so writeError can surface it as a
// structured field in the response's details rather than leaving it embedded only in the
// free-text message.
type missingScopeError struct {
	scope auth.Scope
}

func (e *missingScopeError) Error() string {
	return fmt.Sprintf("%s: this token does not hold %s", ErrForbidden, e.scope)
}

func (e *missingScopeError) Unwrap() error { return ErrForbidden }

func (s *Server) requireScope(want auth.Scope, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal := principalFrom(r.Context())
		if principal == nil || !principal.Scopes.Has(want) {
			annotate(r.Context(), func(a *annotation) {
				a.details = map[string]any{"required_scope": string(want), "reason": "insufficient_scope"}
			})
			s.writeError(w, r, &missingScopeError{scope: want})
			return
		}
		next(w, r)
	}
}
