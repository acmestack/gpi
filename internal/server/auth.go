package server

import (
	"net/http"
	"strings"

	"github.com/acmestack/gpi/internal/state"
)

// authMiddleware validates a bearer token on every request when enabled.
// Public endpoints (healthz, token creation) are excluded so the first token
// can be bootstrapped.
type authMiddleware struct {
	enabled bool
	store   *state.Store
}

func newAuthMiddleware(store *state.Store, enabled bool) *authMiddleware {
	return &authMiddleware{store: store, enabled: enabled}
}

func (m *authMiddleware) Wrap(next http.Handler) http.Handler {
	if !m.enabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Public endpoints: healthz, token creation (bootstrap) and the OpenAPI
		// docs so the Swagger UI can be opened without a token.
		switch {
		case r.URL.Path == "/healthz":
			next.ServeHTTP(w, r)
			return
		case r.URL.Path == "/swagger.json" || r.URL.Path == "/docs" || r.URL.Path == "/redoc":
			next.ServeHTTP(w, r)
			return
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tokens":
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(auth, prefix) {
			writeAuthError(w, http.StatusUnauthorized, errNoToken)
			return
		}
		raw := strings.TrimSpace(strings.TrimPrefix(auth, prefix))
		if raw == "" {
			writeAuthError(w, http.StatusUnauthorized, errNoToken)
			return
		}
		token, err := m.store.GetTokenByHash(state.TokenHash(raw))
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, err)
			return
		}
		// Fire-and-forget last-used update.
		_ = m.store.MarkTokenUsed(token.TokenID)
		next.ServeHTTP(w, r)
	})
}

var errNoToken = &authError{"missing bearer token"}

type authError struct{ msg string }

func (e *authError) Error() string { return e.msg }

func writeAuthError(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"error":"` + err.Error() + `"}`))
}
