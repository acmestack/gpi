package serve

import (
	"context"
	"fmt"
	"time"

	"github.com/acmestack/gpi/internal/backend"
	"github.com/acmestack/gpi/internal/logging"
	"github.com/acmestack/gpi/internal/optimizer"
	"github.com/acmestack/gpi/internal/state"
	"github.com/acmestack/gpi/internal/task"
)

// logger is the package logger, tagged with the module name.
var logger = logging.WithName("serve")

// Manager deploys and tracks replicated services (SkyServe analog).
type Manager struct {
	Store *state.Store
	Prov  *backend.Manager
}

// New returns a service Manager backed by the given store and execution backend.
func New(store *state.Store, prov *backend.Manager) *Manager {
	return &Manager{Store: store, Prov: prov}
}

// Up provisions replicas for a service and tracks it in the store.
func (m *Manager) Up(ctx context.Context, name string, ts *task.Task, plan *optimizer.Plan) (*state.Service, error) {
	if ts.Service == nil {
		return nil, fmt.Errorf("task has no service section")
	}
	replicas := ts.Service.Replicas
	if replicas <= 0 {
		replicas = 1
	}
	port := ts.Service.Port
	if port == 0 {
		return nil, fmt.Errorf("service port is required")
	}

	svc := &state.Service{
		Name:     name,
		Status:   "creating",
		Replicas: replicas,
		Port:     port,
		TaskYAML: ts.String(),
	}
	if err := m.Store.AddService(svc); err != nil {
		return nil, err
	}
	logger.Info("deploying service", "service", name, "replicas", replicas, "port", port)

	for i := 0; i < replicas; i++ {
		replicaName := fmt.Sprintf("serve-%s-%d", name, i)
		replicaTask := *ts
		replicaTask.Name = replicaName
		replicaTask.Service = nil
		replicaTask.Run = serviceRunScript(ts, port)

		cluster, err := m.Prov.Launch(ctx, replicaName, &replicaTask, plan.Launches[0])
		if err != nil {
			m.Store.DeleteCluster(replicaName)
			m.Store.UpdateService(name, func(s *state.Service) error {
				s.Status = "error"
				s.Error = fmt.Sprintf("launch replica %d: %v", i, err)
				return nil
			})
			return nil, fmt.Errorf("launch replica %d: %w", i, err)
		}
		m.Store.UpdateService(name, func(s *state.Service) error {
			s.ReplicaClusters = append(s.ReplicaClusters, replicaName)
			s.Endpoints = append(s.Endpoints, fmt.Sprintf("%s:%d", cluster.GetNodeIP(), port))
			return nil
		})
	}

	m.Store.UpdateService(name, func(s *state.Service) error {
		s.Status = "running"
		s.UpdatedAt = time.Now().Unix()
		return nil
	})
	logger.Info("service running", "service", name)
	return m.Store.GetService(name)
}

func serviceRunScript(ts *task.Task, port int) string {
	run := ts.Service.Run
	if run == "" {
		run = ts.Run
	}
	return fmt.Sprintf("nohup bash -c %q > /root/gpi-service.log 2>&1 & echo $! > /root/gpi-service.pid", run)
}
