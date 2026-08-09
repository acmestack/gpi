package server

import "net/http"

// Middleware wraps an http.Handler, allowing teams to inject custom behavior
// (auth, tracing, rate limiting, headers, etc.) into every request, mirroring
// SkyPilot's FastAPI middleware stack.
type Middleware interface {
	// Wrap returns a handler that wraps next. Implementations typically do
	// work before calling next.ServeHTTP, and/or after it returns.
	Wrap(next http.Handler) http.Handler
}

// MiddlewareFunc adapts a plain function to the Middleware interface.
type MiddlewareFunc func(next http.Handler) http.Handler

func (f MiddlewareFunc) Wrap(next http.Handler) http.Handler { return f(next) }

// chain composes middlewares outermost-first: chain{m1, m2} serves
// m1(m2(handler)).
func chain(handler http.Handler, mws []Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		if mws[i] != nil {
			handler = mws[i].Wrap(handler)
		}
	}
	return handler
}
