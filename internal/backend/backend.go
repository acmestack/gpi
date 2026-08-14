package backend

import (
	"context"

	"github.com/acmestack/gpi/internal/logging"
	"github.com/acmestack/gpi/internal/optimizer"
	"github.com/acmestack/gpi/internal/state"
	"github.com/acmestack/gpi/internal/task"
)

// logger is the package logger, tagged with the module name.
var logger = logging.WithName("backend")

// Backend is an execution backend: how a task's setup/run is executed and
// what a "cluster" means. This mirrors SkyPilot's backend layer (cloud VM,
// existing cluster, docker, local) while staying minimal.
type Backend interface {
	// Name returns the backend identifier (task.BackendCloud, ...).
	Name() string
	// Launch makes the target ready and records a Cluster in the store.
	Launch(ctx context.Context, name string, ts *task.Task, l *optimizer.Launch) (*state.Cluster, error)
	// RunTask runs setup (all nodes) then run (head) for the task.
	RunTask(ctx context.Context, name string, ts *task.Task, stream func(line string)) (int, error)
	// Exec runs an arbitrary command on the cluster's head.
	Exec(ctx context.Context, name, cmd string, stream func(line string)) (int, error)
	// Down tears down the cluster and removes its state record.
	Down(ctx context.Context, name string) error
	// Stop suspends the cluster (billing stops, data retained).
	Stop(ctx context.Context, name string) error
	// Start resumes a stopped cluster.
	Start(ctx context.Context, name string) error
}

// Manager dispatches lifecycle operations to the backend that created a
// cluster, chosen at launch time by task.Backend and recorded on the cluster.
type Manager struct {
	store    *state.Store
	backends map[string]Backend
}

// New builds a Manager registering all execution backends.
func New(store *state.Store, dir string) (*Manager, error) {
	cloud, err := newCloudBackend(store, dir)
	if err != nil {
		return nil, err
	}
	return &Manager{
		store: store,
		backends: map[string]Backend{
			task.BackendCloud:    cloud,
			task.BackendExisting: newExistingBackend(store),
			task.BackendDocker:   newDockerBackend(store),
			task.BackendLocal:    newLocalBackend(store),
		},
	}, nil
}

// Backends lists the names of all registered execution backends.
func (m *Manager) Backends() []string {
	names := make([]string, 0, len(m.backends))
	for n := range m.backends {
		names = append(names, n)
	}
	return names
}

// Launch dispatches to the backend selected by the task.
func (m *Manager) Launch(ctx context.Context, name string, ts *task.Task, l *optimizer.Launch) (*state.Cluster, error) {
	b, ok := m.backends[ts.Backend]
	if !ok {
		return nil, errUnknownBackend(ts.Backend)
	}
	cluster, err := b.Launch(ctx, name, ts, l)
	if err != nil {
		return nil, err
	}
	// Record the launch snapshot (yaml + history) regardless of backend.
	if err := m.recordLaunch(cluster, ts, l); err != nil {
		return nil, err
	}
	_ = m.store.AddClusterEvent(&state.ClusterEvent{
		ClusterName:    cluster.Name,
		EndingStatus:   string(cluster.Status),
		Type:           state.EventLaunch,
		Reason:         "cluster launched",
		TransitionedAt: cluster.CreatedAt,
	})
	return cluster, nil
}

// recordLaunch persists the task YAML snapshot and launch history for a cluster
// (mirrors SkyPilot's cluster_yaml + cluster_history tables).
func (m *Manager) recordLaunch(cluster *state.Cluster, ts *task.Task, l *optimizer.Launch) error {
	yamlStr := ts.String()
	if err := m.store.RecordClusterYAML(cluster.Name, yamlStr); err != nil {
		return err
	}
	instanceType := ""
	zone := ""
	if l != nil {
		instanceType = l.InstanceType
		zone = l.Zone
	}
	return m.store.RecordClusterHistory(&state.ClusterHistory{
		ClusterName:  cluster.Name,
		NumNodes:     cluster.NumNodes,
		Cloud:        cluster.Cloud,
		Region:       cluster.Region,
		Zone:         zone,
		InstanceType: instanceType,
		Backend:      cluster.Backend,
		TaskYAML:     yamlStr,
		LaunchedAt:   cluster.CreatedAt,
	})
}

// RunTask dispatches to the backend that owns the named cluster.
func (m *Manager) RunTask(ctx context.Context, name string, ts *task.Task, stream func(line string)) (int, error) {
	b, err := m.forCluster(name)
	if err != nil {
		return -1, err
	}
	return b.RunTask(ctx, name, ts, stream)
}

// Exec dispatches to the backend that owns the named cluster.
func (m *Manager) Exec(ctx context.Context, name, cmd string, stream func(line string)) (int, error) {
	b, err := m.forCluster(name)
	if err != nil {
		return -1, err
	}
	return b.Exec(ctx, name, cmd, stream)
}

// Down dispatches to the backend that owns the named cluster.
func (m *Manager) Down(ctx context.Context, name string) error {
	b, err := m.forCluster(name)
	if err != nil {
		return err
	}
	return b.Down(ctx, name)
}

// Stop dispatches to the backend that owns the named cluster.
func (m *Manager) Stop(ctx context.Context, name string) error {
	b, err := m.forCluster(name)
	if err != nil {
		return err
	}
	return b.Stop(ctx, name)
}

// Start dispatches to the backend that owns the named cluster.
func (m *Manager) Start(ctx context.Context, name string) error {
	b, err := m.forCluster(name)
	if err != nil {
		return err
	}
	return b.Start(ctx, name)
}

// GpiletHealth reports live node health for a cluster; only meaningful for the
// cloud backend (gpilet agent). Other backends report "n/a".
func (m *Manager) GpiletHealth(ctx context.Context, cluster *state.Cluster, node *state.Node) string {
	if cluster.Backend == "" || cluster.Backend == task.BackendCloud {
		if cb, ok := m.backends[task.BackendCloud].(*cloudBackend); ok {
			return cb.GpiletHealth(ctx, cluster, node)
		}
		return "n/a"
	}
	return "n/a (backend: " + cluster.Backend + ")"
}

func (m *Manager) forCluster(name string) (Backend, error) {
	cluster, err := m.store.GetCluster(name)
	if err != nil {
		return nil, err
	}
	bname := cluster.Backend
	if bname == "" {
		bname = task.BackendCloud
	}
	b, ok := m.backends[bname]
	if !ok {
		return nil, errUnknownBackend(bname)
	}
	return b, nil
}

func errUnknownBackend(name string) error {
	return &UnknownBackendError{Name: name}
}
