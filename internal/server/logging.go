package server

import (
	"net/http"
	"time"

	"github.com/acmestack/gpi/internal/logging"
)

// loggingMiddleware logs each request method/path/duration. Health checks
// (/healthz) are skipped to avoid noise from frequent probes.
type loggingMiddleware struct {
	logger *logging.Logger
}

func (m *loggingMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := nowMs()
		next.ServeHTTP(w, r)
		if m.logger == nil || r.URL.Path == "/healthz" {
			return
		}
		m.logger.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration_ms", nowMs()-start,
		)
	})
}

func nowMs() int64 {
	return time.Now().UnixMilli()
}
