package state

// ClusterEventType classifies a cluster status-change event.
type ClusterEventType string

const (
	// EventLaunch records a launch/status transition.
	EventLaunch ClusterEventType = "launch"
	// EventStop records a stop transition.
	EventStop ClusterEventType = "stop"
	// EventStart records a start transition.
	EventStart ClusterEventType = "start"
	// EventDown records a termination transition.
	EventDown ClusterEventType = "down"
	// EventStatusChange records a generic status change.
	EventStatusChange ClusterEventType = "status_change"
)

// ClusterEvent is a single transition in a cluster's lifecycle. Mirrors
// SkyPilot's cluster_events table, carrying a request_id for traceability.
type ClusterEvent struct {
	ClusterName    string           `json:"cluster_name"`
	StartingStatus string           `json:"starting_status"`
	EndingStatus   string           `json:"ending_status"`
	Reason         string           `json:"reason,omitempty"`
	Type           ClusterEventType `json:"type"`
	RequestID      string           `json:"request_id,omitempty"`
	TransitionedAt int64            `json:"transitioned_at"`
}

func (e *ClusterEvent) createdAt() int64 { return e.TransitionedAt }
func (e *ClusterEvent) updatedAt() int64 { return e.TransitionedAt }
