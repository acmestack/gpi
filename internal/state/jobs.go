package state

import (
	"errors"
	"time"
)

// Job is a persistent record of a scheduled/on-demand task (Sky Jobs analog).
type Job struct {
	Name       string `json:"name"`
	TaskYAML   string `json:"task_yaml"`
	Schedule   string `json:"schedule"`
	TaskPath   string `json:"task_path,omitempty"`
	Retries    int    `json:"retries"`
	Cluster    string `json:"cluster,omitempty"`
	Status     string `json:"status"`
	RunCount   int    `json:"run_count"`
	FailCount  int    `json:"fail_count"`
	LastRun    int64  `json:"last_run"`
	LastStatus string `json:"last_status,omitempty"`
	NextRun    int64  `json:"next_run"`
	Error      string `json:"error,omitempty"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

func (j *Job) createdAt() int64 { return j.CreatedAt }
func (j *Job) updatedAt() int64 { return j.UpdatedAt }

// AddJob stores a new job with timestamps and persists the change.
func (s *Store) AddJob(job *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.jobs[job.Name]; exists {
		return errors.New("job already exists")
	}
	now := time.Now().Unix()
	job.CreatedAt = now
	job.UpdatedAt = now
	s.jobs[job.Name] = job
	return s.save()
}

// GetJob returns the named job, or an error if it does not exist.
func (s *Store) GetJob(name string) (*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[name]
	if !ok {
		return nil, errors.New("job not found")
	}
	return job, nil
}

// ListJobs returns all jobs.
func (s *Store) ListJobs() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		out = append(out, job)
	}
	return out
}

// UpdateJob applies fn to the named job and persists the change.
func (s *Store) UpdateJob(name string, fn func(*Job) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[name]
	if !ok {
		return errors.New("job not found")
	}
	if err := fn(job); err != nil {
		return err
	}
	job.UpdatedAt = time.Now().Unix()
	return s.save()
}
