package server

import (
	"net/http"
	"time"
)

// handleCreateToken issues a new service account token.
func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		Creator   string `json:"creator"`
		ExpiresIn int64  `json:"expires_in"` // seconds, 0 = no expiry
	}
	if err := decodeJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" {
		req.Name = "default"
	}
	var expiresAt int64
	if req.ExpiresIn > 0 {
		expiresAt = time.Now().Unix() + req.ExpiresIn
	}
	token, secret, err := s.Store.CreateToken(req.Name, req.Creator, expiresAt)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	// The plaintext secret is returned exactly once.
	s.writeJSON(w, http.StatusCreated, map[string]any{
		"token_id":   token.TokenID,
		"token_name": token.TokenName,
		"token":      secret,
		"expires_at": expiresAt,
	})
}

// handleListTokens lists all service account tokens (no secrets).
func (s *Server) handleListTokens(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, s.Store.ListTokens())
}

// handleDeleteToken revokes a token by id.
func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ok, err := s.Store.DeleteToken(id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		s.writeError(w, http.StatusNotFound, errTokenNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRotateToken rotates a token's secret and optional expiration.
func (s *Server) handleRotateToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		ExpiresIn int64 `json:"expires_in"`
	}
	_ = decodeJSON(r, &req)
	var expiresAt int64
	if req.ExpiresIn > 0 {
		expiresAt = time.Now().Unix() + req.ExpiresIn
	}
	token, secret, err := s.Store.RotateToken(id, expiresAt)
	if err != nil {
		s.writeError(w, http.StatusNotFound, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"token_id":   token.TokenID,
		"token_name": token.TokenName,
		"token":      secret,
		"expires_at": expiresAt,
	})
}

// handleGetConfig returns a single config key.
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	s.writeJSON(w, http.StatusOK, map[string]string{"key": key, "value": s.Store.GetConfig(key)})
}

// handleSetConfig upserts a config key.
func (s *Server) handleSetConfig(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	var req struct {
		Value string `json:"value"`
	}
	if err := decodeJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.Store.SetConfig(key, req.Value); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"key": key, "value": req.Value})
}

// handleListConfig lists all config entries.
func (s *Server) handleListConfig(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, s.Store.ListConfig())
}

var errTokenNotFound = &authError{"token not found"}
