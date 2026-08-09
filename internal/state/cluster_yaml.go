package state

// ClusterYAML stores the full task YAML snapshot used to create/launch a
// cluster, so the exact configuration can be recalled later. Mirrors
// SkyPilot's cluster_yaml table.
type ClusterYAML struct {
	ClusterName string `json:"cluster_name"`
	YAML        string `json:"yaml"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

func (c *ClusterYAML) createdAt() int64 { return c.CreatedAt }
func (c *ClusterYAML) updatedAt() int64 { return c.UpdatedAt }
