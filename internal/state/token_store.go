package state

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

// TokenHash computes the sha256 hex digest of a raw token, used for storage
// and lookup.
func TokenHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// GenerateTokenID returns a random token identifier.
func GenerateTokenID() string {
	return randomHex(8)
}

// GenerateTokenSecret returns a random opaque bearer token (40 hex chars).
func GenerateTokenSecret() string {
	return randomHex(20)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// CreateToken registers a new service account token and returns the raw
// (plaintext) token secret, shown to the caller exactly once.
func (s *Store) CreateToken(name string, creator string, expiresAt int64) (*ServiceAccountToken, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	secret := GenerateTokenSecret()
	t := &ServiceAccountToken{
		TokenID:   GenerateTokenID(),
		TokenName: name,
		TokenHash: TokenHash(secret),
		CreatedAt: time.Now().Unix(),
		ExpiresAt: expiresAt,
		Creator:   creator,
		Active:    true,
	}
	s.tokens = append(s.tokens, t)
	if err := s.save(); err != nil {
		return nil, "", err
	}
	return t, secret, nil
}

// GetTokenByHash looks up an active, non-expired token by its hash.
func (s *Store) GetTokenByHash(hash string) (*ServiceAccountToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now().Unix()
	for _, t := range s.tokens {
		if t.TokenHash == hash {
			if t.Expired(now) {
				return nil, errors.New("token expired")
			}
			return t, nil
		}
	}
	return nil, errors.New("token not found")
}

// ListTokens returns all tokens (including expired ones).
func (s *Store) ListTokens() []*ServiceAccountToken {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*ServiceAccountToken, len(s.tokens))
	copy(out, s.tokens)
	return out
}

// DeleteToken removes a token by id, revoking it.
func (s *Store) DeleteToken(tokenID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.tokens {
		if t.TokenID == tokenID {
			s.tokens = append(s.tokens[:i], s.tokens[i+1:]...)
			return true, s.save()
		}
	}
	return false, nil
}

// RotateToken replaces a token's secret and expiration, keeping its id.
func (s *Store) RotateToken(tokenID string, newExpiresAt int64) (*ServiceAccountToken, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	secret := GenerateTokenSecret()
	now := time.Now().Unix()
	for _, t := range s.tokens {
		if t.TokenID == tokenID {
			t.TokenHash = TokenHash(secret)
			t.ExpiresAt = newExpiresAt
			t.CreatedAt = now
			t.LastUsedAt = 0
			if err := s.save(); err != nil {
				return nil, "", err
			}
			return t, secret, nil
		}
	}
	return nil, "", errors.New("token not found")
}

// MarkTokenUsed refreshes a token's last_used_at timestamp.
func (s *Store) MarkTokenUsed(tokenID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	for _, t := range s.tokens {
		if t.TokenID == tokenID {
			t.LastUsedAt = now
			return s.save()
		}
	}
	return nil
}
