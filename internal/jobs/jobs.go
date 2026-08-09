package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/acmestack/gpi/internal/backend"
	"github.com/acmestack/gpi/internal/optimizer"
	"github.com/acmestack/gpi/internal/state"
	"github.com/acmestack/gpi/internal/task"
)

// Manager registers and runs scheduled/on-demand jobs (Sky Jobs analog).
type Manager struct {
	Store *state.Store
	Prov  *backend.Manager
}

// New returns a job Manager backed by the given store and execution backend.
func New(store *state.Store, prov *backend.Manager) *Manager {
	return &Manager{Store: store, Prov: prov}
}

// Submit registers a new job from a task file, either scheduled or queued.
func (m *Manager) Submit(name, taskPath, schedule string, retries int) (*state.Job, error) {
	ts, err := task.Load(taskPath)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = ts.Name
	}

	job := &state.Job{
		Name:     name,
		TaskYAML: ts.String(),
		TaskPath: taskPath,
		Schedule: schedule,
		Retries:  retries,
		Status:   "registered",
	}

	if schedule == "" {
		job.Status = "queued"
	} else {
		if _, dur, err := parseSchedule(schedule); err != nil {
			return nil, fmt.Errorf("invalid schedule %q: %w", schedule, err)
		} else if dur > 0 {
			job.NextRun = time.Now().Add(dur).Unix()
		} else {
			job.NextRun = time.Now().Unix()
		}
	}
	m.Store.AddJob(job)
	return job, nil
}

// RunNow executes the named job's task, retrying per its configured retries.
func (m *Manager) RunNow(ctx context.Context, name string, stream func(string)) error {
	job, err := m.Store.GetJob(name)
	if err != nil {
		return err
	}
	ts, err := task.Load(job.TaskPath)
	if err != nil {
		return err
	}
	cluster := job.Cluster
	if cluster == "" {
		cluster = "job-" + name
	}

	m.Store.UpdateJob(name, func(j *state.Job) error {
		j.Status = "running"
		j.LastRun = time.Now().Unix()
		return nil
	})

	var lastErr error
	for attempt := 0; attempt <= job.Retries; attempt++ {
		if attempt > 0 && stream != nil {
			stream(fmt.Sprintf("[retry %d/%d]", attempt, job.Retries))
		}
		lastErr = m.runOnce(ctx, name, ts, cluster, stream)
		if lastErr == nil {
			m.Store.UpdateJob(name, func(j *state.Job) error {
				j.Status = "done"
				j.RunCount++
				j.LastStatus = "success"
				return nil
			})
			return nil
		}
		m.Store.UpdateJob(name, func(j *state.Job) error {
			j.FailCount++
			j.LastStatus = "failed"
			j.Error = lastErr.Error()
			return nil
		})
	}
	m.Store.UpdateJob(name, func(j *state.Job) error {
		j.Status = "failed"
		return nil
	})
	return lastErr
}

func (m *Manager) runOnce(ctx context.Context, name string, ts *task.Task, cluster string, stream func(string)) error {
	var launch *optimizer.Launch
	if ts.Backend != task.BackendCloud {
		launch = &optimizer.Launch{Cloud: ts.Backend, NumNodes: ts.NumNodes}
	} else {
		plan, err := optimizer.Default().Optimize(ctx, &optimizer.Request{
			Resources: ts.Resources,
			Options:   &optimizer.Options{NumNodes: ts.NumNodes},
		})
		if err != nil {
			return err
		}
		launch = plan.Launches[0]
	}
	if _, err := m.Prov.Launch(ctx, cluster, ts, launch); err != nil {
		return err
	}
	_, err := m.Prov.RunTask(ctx, cluster, ts, stream)
	return err
}

// Due returns scheduled jobs whose next run time has arrived.
func (m *Manager) Due() []*state.Job {
	var due []*state.Job
	now := time.Now()
	for _, job := range m.Store.ListJobs() {
		if job.Schedule == "" {
			continue
		}
		if job.Status == "running" || job.Status == "queued" {
			continue
		}
		if job.NextRun > 0 && now.Unix() >= job.NextRun {
			due = append(due, job)
		}
	}
	return due
}

func (m *Manager) computeNext(job *state.Job) time.Time {
	sched, dur, err := parseSchedule(job.Schedule)
	if err != nil {
		return time.Time{}
	}
	if dur > 0 {
		return time.Now().Add(dur)
	}
	return sched.next(time.Now())
}

// Reschedule computes and stores the job's next run time after completion.
func (m *Manager) Reschedule(job *state.Job) error {
	return m.Store.UpdateJob(job.Name, func(j *state.Job) error {
		next := m.computeNext(j)
		if next.IsZero() {
			j.Status = "stopped"
			j.NextRun = 0
			return nil
		}
		j.NextRun = next.Unix()
		j.Status = "registered"
		return nil
	})
}
