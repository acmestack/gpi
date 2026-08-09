package state

import (
	"errors"
	"time"
)

// Service is a persistent record of a replicated deployment (SkyServe analog).
type Service struct {
	Name            string   `json:"name"`
	Status          string   `json:"status"`
	Replicas        int      `json:"replicas"`
	Port            int      `json:"port"`
	Endpoints       []string `json:"endpoints"`
	ReplicaClusters []string `json:"replica_clusters"`
	TaskYAML        string   `json:"task_yaml"`
	Error           string   `json:"error,omitempty"`
	CreatedAt       int64    `json:"created_at"`
	UpdatedAt       int64    `json:"updated_at"`
}

func (s *Service) createdAt() int64 { return s.CreatedAt }
func (s *Service) updatedAt() int64 { return s.UpdatedAt }

// AddService stores a new service with timestamps and persists the change.
func (s *Store) AddService(svc *Service) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.services[svc.Name]; exists {
		return errors.New("service already exists")
	}
	now := time.Now().Unix()
	svc.CreatedAt = now
	svc.UpdatedAt = now
	s.services[svc.Name] = svc
	return s.save()
}

// GetService returns the named service, or an error if it does not exist.
func (s *Store) GetService(name string) (*Service, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	svc, ok := s.services[name]
	if !ok {
		return nil, errors.New("service not found")
	}
	return svc, nil
}

// ListServices returns all services.
func (s *Store) ListServices() []*Service {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Service, 0, len(s.services))
	for _, svc := range s.services {
		out = append(out, svc)
	}
	return out
}

// UpdateService applies fn to the named service and persists the change.
func (s *Store) UpdateService(name string, fn func(*Service) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	svc, ok := s.services[name]
	if !ok {
		return errors.New("service not found")
	}
	if err := fn(svc); err != nil {
		return err
	}
	svc.UpdatedAt = time.Now().Unix()
	return s.save()
}

// DeleteService removes the named service and persists the change.
func (s *Store) DeleteService(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.services[name]; !ok {
		return errors.New("service not found")
	}
	delete(s.services, name)
	return s.save()
}
