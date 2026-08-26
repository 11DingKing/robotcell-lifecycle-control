package httpapi

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/auth"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/service"
)

type Middleware struct {
	auth   *auth.Service
	logger *slog.Logger
}

func NewMiddleware(a *auth.Service, l *slog.Logger) *Middleware {
	return &Middleware{auth: a, logger: l}
}

func (m *Middleware) Base(next http.Handler) http.Handler {
	return m.recoverPanic(m.requestID(m.logging(next)))
}

func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(m.logger, w, r, serviceAuthenticationError())
			return
		}
		principal, err := m.auth.Authenticate(r.Context(), strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			writeError(m.logger, w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(service.ContextWithPrincipal(r.Context(), principal)))
	})
}

func (m *Middleware) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if id == "" || len(id) > 128 {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(withRequestID(r.Context(), id)))
	})
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (m *Middleware) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &responseRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(recorder, r)
		m.logger.Info("http request", "request_id", RequestID(r.Context()), "method", r.Method, "path", r.URL.Path, "status", recorder.status, "duration_ms", time.Since(started).Milliseconds())
	})
}

func (m *Middleware) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				m.logger.Error("panic recovered", "request_id", RequestID(r.Context()), "panic", recovered, "stack", string(debug.Stack()))
				writeError(m.logger, w, r, panicError())
			}
		}()
		next.ServeHTTP(w, r)
	})
}
