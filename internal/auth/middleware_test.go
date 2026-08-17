package auth_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getorcal/orcal/internal/auth"
)

func protected(t *testing.T) http.Handler {
	t.Helper()
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return auth.Middleware(auth.HashToken("secret"))(ok)
}

func TestMiddlewareAcceptsCorrectBearerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/sandboxes", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	protected(t).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestMiddlewareRejectsMissingWrongAndMalformedHeaders(t *testing.T) {
	cases := []struct{ name, header string }{
		{"missing", ""},
		{"wrong token", "Bearer nope"},
		{"no scheme", "secret"},
		{"wrong scheme", "Basic secret"},
		{"empty token", "Bearer "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/sandboxes", nil)
			if c.header != "" {
				req.Header.Set("Authorization", c.header)
			}
			rec := httptest.NewRecorder()

			protected(t).ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), `"unauthorized"`) {
				t.Errorf("body = %q, want the unauthorized error code", rec.Body.String())
			}
		})
	}
}

func TestMiddlewareIsCaseInsensitiveOnScheme(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/sandboxes", nil)
	req.Header.Set("Authorization", "bearer secret")
	rec := httptest.NewRecorder()

	protected(t).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 for a lowercase scheme", rec.Code)
	}
}
