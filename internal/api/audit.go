package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/getorcal/orcal/internal/audit"
)

const annotationKey contextKey = "audit_annotation"

type annotation struct {
	action       audit.Action
	resourceType string
	resourceID   string
	details      map[string]any
	audited      bool
	actorTokenID string
	actorName    string
}

func annotate(ctx context.Context, fn func(*annotation)) {
	if a, ok := ctx.Value(annotationKey).(*annotation); ok {
		fn(a)
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.written {
		r.status = status
		r.written = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.written {
		r.status = http.StatusOK
		r.written = true
	}
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (s *Server) auditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := &annotation{}
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		ctx := context.WithValue(r.Context(), annotationKey, a)

		next.ServeHTTP(recorder, r.WithContext(ctx))

		event, ok := s.buildEvent(r, recorder.status, a)
		if !ok {
			return
		}
		if err := s.audit.Record(r.Context(), event); err != nil {
			s.logger.ErrorContext(r.Context(), "audit event not recorded",
				slog.String("error", err.Error()),
				slog.String("action", string(event.Action)),
				slog.String("request_id", event.RequestID))
		}
	})
}

func (s *Server) buildEvent(r *http.Request, status int, a *annotation) (*audit.Event, bool) {
	denied := status == http.StatusUnauthorized || status == http.StatusForbidden
	if !denied && !a.audited {
		return nil, false
	}

	event := &audit.Event{
		Action:       a.action,
		ResourceType: a.resourceType,
		ResourceID:   a.resourceID,
		RequestID:    requestIDFrom(r.Context()),
		Status:       status,
		RemoteAddr:   r.RemoteAddr,
		Details:      a.details,
	}
	event.ActorTokenID = a.actorTokenID
	event.ActorName = a.actorName
	if denied {
		event.Action = audit.ActionAuthDenied
	}
	if event.Details == nil {
		event.Details = map[string]any{}
	}
	return event, true
}

func (s *Server) withRoute(r route, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		annotate(req.Context(), func(a *annotation) {
			a.action = r.Action
			a.audited = r.Audited
		})
		next(w, req)
	}
}
