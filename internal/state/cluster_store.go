package state

import (
	"errors"
	"time"
)

// RecordClusterYAML stores (upserts) the task YAML snapshot for a cluster.
func (s *Store) RecordClusterYAML(clusterName, yamlStr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	if existing, ok := s.clusterYAMLs[clusterName]; ok {
		existing.YAML = yamlStr
		existing.UpdatedAt = now
	} else {
		s.clusterYAMLs[clusterName] = &ClusterYAML{
			ClusterName: clusterName,
			YAML:        yamlStr,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
	}
	return s.save()
}

// GetClusterYAML returns the stored task YAML snapshot for a cluster.
func (s *Store) GetClusterYAML(clusterName string) (*ClusterYAML, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	y, ok := s.clusterYAMLs[clusterName]
	if !ok {
		return nil, errors.New("cluster yaml not found")
	}
	return y, nil
}

// ListClusterYAMLs returns all stored cluster YAML snapshots.
func (s *Store) ListClusterYAMLs() []*ClusterYAML {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*ClusterYAML, 0, len(s.clusterYAMLs))
	for _, y := range s.clusterYAMLs {
		out = append(out, y)
	}
	return out
}

// RecordClusterHistory upserts the launch facts of a cluster.
func (s *Store) RecordClusterHistory(h *ClusterHistory) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	if existing, ok := s.clusterHistory[h.ClusterName]; ok {
		existing.NumNodes = h.NumNodes
		existing.Cloud = h.Cloud
		existing.Region = h.Region
		existing.Zone = h.Zone
		existing.InstanceType = h.InstanceType
		existing.Backend = h.Backend
		existing.TaskYAML = h.TaskYAML
		existing.LaunchedAt = h.LaunchedAt
		existing.UpdatedAt = now
	} else {
		if h.CreatedAt == 0 {
			h.CreatedAt = now
		}
		if h.LaunchedAt == 0 {
			h.LaunchedAt = now
		}
		h.UpdatedAt = now
		s.clusterHistory[h.ClusterName] = h
	}
	return s.save()
}

// ListClusterHistory returns all recorded cluster launch histories.
func (s *Store) ListClusterHistory() []*ClusterHistory {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*ClusterHistory, 0, len(s.clusterHistory))
	for _, h := range s.clusterHistory {
		out = append(out, h)
	}
	return out
}

// AddClusterEvent appends a lifecycle event for a cluster.
func (s *Store) AddClusterEvent(e *ClusterEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.TransitionedAt == 0 {
		e.TransitionedAt = time.Now().Unix()
	}
	s.clusterEvents = append(s.clusterEvents, e)
	return s.save()
}

// ListClusterEvents returns the lifecycle events of all clusters.
func (s *Store) ListClusterEvents() []*ClusterEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*ClusterEvent, len(s.clusterEvents))
	copy(out, s.clusterEvents)
	return out
}

// ListClusterEventsFor returns lifecycle events for a single cluster.
func (s *Store) ListClusterEventsFor(clusterName string) []*ClusterEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*ClusterEvent
	for _, e := range s.clusterEvents {
		if e.ClusterName == clusterName {
			out = append(out, e)
		}
	}
	return out
}
