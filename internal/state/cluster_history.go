package state

// ClusterHistory records the launch facts of a cluster, kept after the cluster
// is torn down for cost/usage reporting. Mirrors SkyPilot's cluster_history
// table (without per-usage-interval tracking).
type ClusterHistory struct {
	ClusterName  string `json:"cluster_name"`
	NumNodes     int    `json:"num_nodes"`
	Cloud        string `json:"cloud"`
	Region       string `json:"region"`
	Zone         string `json:"zone"`
	InstanceType string `json:"instance_type"`
	Backend      string `json:"backend"`
	TaskYAML     string `json:"task_yaml,omitempty"`
	LaunchedAt   int64  `json:"launched_at"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

func (c *ClusterHistory) createdAt() int64 { return c.CreatedAt }
func (c *ClusterHistory) updatedAt() int64 { return c.UpdatedAt }
