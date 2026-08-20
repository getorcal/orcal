package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/getorcal/orcal/internal/apigen"
	"github.com/getorcal/orcal/internal/auth"
)

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var req apigen.CreateTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, r, fmt.Errorf("%w: malformed JSON body", ErrInvalidRequest))
		return
	}

	opts := auth.CreateOptions{Name: req.Name, Scopes: make(auth.Scopes, 0, len(req.Scopes))}
	for _, scope := range req.Scopes {
		opts.Scopes = append(opts.Scopes, auth.Scope(scope))
	}
	if req.ExpiresInSeconds != nil {
		opts.ExpiresIn = time.Duration(*req.ExpiresInSeconds) * time.Second
	}

	principal := principalFrom(r.Context())
	if principal == nil {
		s.writeError(w, r, ErrForbidden)
		return
	}

	created, plaintext, err := s.tokens.Create(r.Context(), opts, principal.Scopes)
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	annotate(r.Context(), func(a *annotation) {
		a.resourceType = "token"
		a.resourceID = created.ID
		scopes := make([]string, len(created.Scopes))
		for i, scope := range created.Scopes {
			scopes[i] = string(scope)
		}
		a.details = map[string]any{"token_id": created.ID, "scopes": scopes}
	})
	writeJSON(w, http.StatusCreated, apigen.CreatedToken{Token: plaintext, Id: created.ID, Name: created.Name,
		Prefix: created.Prefix, Scopes: apiScopes(created.Scopes), CreatedAt: created.CreatedAt,
		ExpiresAt: created.ExpiresAt, LastUsedAt: created.LastUsedAt, RevokedAt: created.RevokedAt})
}

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.tokens.List(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	out := apigen.TokenList{Items: make([]apigen.Token, 0, len(tokens))}
	for _, tok := range tokens {
		out.Items = append(out.Items, toAPIToken(tok))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.tokens.Revoke(r.Context(), id); err != nil {
		s.writeError(w, r, err)
		return
	}
	annotate(r.Context(), func(a *annotation) {
		a.resourceType = "token"
		a.resourceID = id
	})
	w.WriteHeader(http.StatusNoContent)
}
