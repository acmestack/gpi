package state

// ConfigEntry is a single key-value configuration record, persisted so runtime
// settings survive restarts and are shared across server instances. Mirrors
// SkyPilot's config table.
type ConfigEntry struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

func (c *ConfigEntry) createdAt() int64 { return c.CreatedAt }
func (c *ConfigEntry) updatedAt() int64 { return c.UpdatedAt }
