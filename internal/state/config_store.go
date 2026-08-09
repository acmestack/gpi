package state

import "time"

// GetConfig returns a config value by key, or "" if unset.
func (s *Store) GetConfig(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if e, ok := s.config[key]; ok {
		return e.Value
	}
	return ""
}

// SetConfig upserts a config key/value pair.
func (s *Store) SetConfig(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	if e, ok := s.config[key]; ok {
		e.Value = value
		e.UpdatedAt = now
	} else {
		s.config[key] = &ConfigEntry{Key: key, Value: value, CreatedAt: now, UpdatedAt: now}
	}
	return s.save()
}

// ListConfig returns all config entries.
func (s *Store) ListConfig() []*ConfigEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*ConfigEntry, 0, len(s.config))
	for _, e := range s.config {
		out = append(out, e)
	}
	return out
}
