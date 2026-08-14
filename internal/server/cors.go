package server

import "net/http"

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
