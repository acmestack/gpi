package server

import (
	"net/http"
	"time"
)

// corsMiddleware adds permissive CORS headers (SkyPilot defaults to '*').
type corsMiddleware struct {
	allowedOrigins []string
}

func newCORSMiddleware(origins []string) *corsMiddleware {
	if len(origins) == 0 {
		origins = []string{"*"}
	}
	return &corsMiddleware{allowedOrigins: origins}
}

func (m *corsMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		origin := r.Header.Get("Origin")
		allowed := "*"
		if !contains(m.allowedOrigins, "*") {
			allowed = origin
		}
		h.Set("Access-Control-Allow-Origin", allowed)
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Origin, X-Request-Id")
		h.Set("Access-Control-Expose-Headers", "X-Request-Id")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// securityHeadersMiddleware adds basic security headers to all responses.
type securityHeadersMiddleware struct{}

func (*securityHeadersMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs each request method/path/duration.
type loggingMiddleware struct {
	logf func(string, ...any)
}

func (m *loggingMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := nowMs()
		next.ServeHTTP(w, r)
		if m.logf != nil {
			m.logf("%s %s (%dms)", r.Method, r.URL.Path, int(nowMs()-start))
		}
	})
}

func nowMs() int64 {
	return time.Now().UnixMilli()
}

func unixMillis() int64 {
	return time.Now().UnixMilli()
}
