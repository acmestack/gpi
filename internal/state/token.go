package state

import "time"

// ServiceAccountToken is an API access token used to authenticate requests to
// the gpi server REST API. Only the sha256 hash of the token is stored; the
// plaintext is shown once at creation. Mirrors SkyPilot's
// service_account_tokens table.
type ServiceAccountToken struct {
	TokenID    string `json:"token_id"`
	TokenName  string `json:"token_name"`
	TokenHash  string `json:"token_hash"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt int64  `json:"last_used_at"`
	ExpiresAt  int64  `json:"expires_at"`
	Creator    string `json:"creator"`
	// Active is derived at load: a token is active when not expired and not
	// explicitly revoked (revocation deletes the row).
	Active bool `json:"active,omitempty"`
}

func (t *ServiceAccountToken) createdAt() int64 { return t.CreatedAt }
func (t *ServiceAccountToken) updatedAt() int64 {
	if t.LastUsedAt > t.CreatedAt {
		return t.LastUsedAt
	}
	return t.CreatedAt
}

// Expired reports whether the token is past its expiration timestamp.
// A zero ExpiresAt means the token never expires.
func (t *ServiceAccountToken) Expired(at int64) bool {
	return t.ExpiresAt > 0 && at > t.ExpiresAt
}

func nowUnix() int64 { return time.Now().Unix() }
