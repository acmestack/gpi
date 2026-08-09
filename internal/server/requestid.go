package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
)

// DefaultRequestIDHeader is the header key used to carry the request ID unless
// overridden via GPI_REQUEST_ID_HEADER or SetRequestIDHeader.
const DefaultRequestIDHeader = "x-request-id"

// DefaultRequestIDBodyField is the JSON field injected into response bodies.
const DefaultRequestIDBodyField = "request_id"

type requestIDKey struct{}

// RequestIDFrom extracts the request ID associated with a request's context.
// It returns the incoming header value if the upstream server supplied one,
// or the generated ID otherwise.
func RequestIDFrom(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

func withRequestID(headerKey string, next http.Handler) http.Handler {
	return requestIDMiddleware{headerKey: headerKey}.Wrap(next)
}

// requestIDMiddleware adds a request ID (pass-through or generated) to the
// response header and request context.
type requestIDMiddleware struct {
	headerKey string
}

func (m requestIDMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(m.headerKey)
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set(m.headerKey, id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(&reqIDWriter{ResponseWriter: w, id: id}, r.WithContext(ctx))
	})
}

// reqIDWriter decorates the ResponseWriter so handlers can recover the request
// ID for body injection without changing their signatures.
type reqIDWriter struct {
	http.ResponseWriter
	id string
}

// requestIDOf returns the request ID carried by a (possibly wrapped)
// ResponseWriter, or "" when unavailable.
func requestIDOf(w http.ResponseWriter) string {
	if rw, ok := w.(*reqIDWriter); ok {
		return rw.id
	}
	return ""
}

// newRequestID generates a 32-hex-char random request ID.
func newRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// validateHeaderKey rejects header keys that are not valid HTTP tokens, so a
// misconfigured GPI_REQUEST_ID_HEADER fails fast at startup.
func validateHeaderKey(key string) error {
	if key == "" {
		return fmt.Errorf("request id header key must not be empty")
	}
	for _, c := range key {
		if c <= 32 || c >= 127 {
			return fmt.Errorf("invalid request id header key %q", key)
		}
		switch c {
		case '(', ')', '<', '>', '@', ',', ';', ':', '\\', '"', '/', '[', ']', '?', '=', '{', '}':
			return fmt.Errorf("invalid request id header key %q", key)
		}
	}
	return nil
}

// injectRequestID adds the request ID into the response body. If the body is a
// map it inserts a request_id field directly; otherwise it wraps the payload so
// the ID is always present.
func injectRequestID(body any, id, field string) any {
	if id == "" {
		return body
	}
	if m, ok := body.(map[string]any); ok {
		m[field] = id
		return m
	}
	if m, ok := body.(map[string]string); ok {
		out := make(map[string]any, len(m)+1)
		for k, v := range m {
			out[k] = v
		}
		out[field] = id
		return out
	}
	return map[string]any{
		field:  id,
		"data": body,
	}
}
