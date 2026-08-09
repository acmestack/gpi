package backend

import (
	"context"

	"github.com/acmestack/gpi/internal/optimizer"
	"github.com/acmestack/gpi/internal/provisioner"
	"github.com/acmestack/gpi/internal/state"
	"github.com/acmestack/gpi/internal/task"
)

// cloudBackend provisions cloud VMs (the default and original backend),
// delegating to the provisioner package.
type cloudBackend struct {
	p     *provisioner.Provisioner
	store *state.Store
}

func newCloudBackend(store *state.Store, dir string) (*cloudBackend, error) {
	return &cloudBackend{p: provisioner.New(store, dir), store: store}, nil
}

func (c *cloudBackend) Name() string { return task.BackendCloud }

func (c *cloudBackend) Launch(ctx context.Context, name string, ts *task.Task, l *optimizer.Launch) (*state.Cluster, error) {
	cluster, err := c.p.Launch(ctx, name, ts, l)
	if err != nil {
		return nil, err
	}
	// Record the backend on the persisted cluster so later lifecycle
	// operations dispatch back to this backend.
	return cluster, c.store.UpdateCluster(cluster.Name, func(cl *state.Cluster) error {
		cl.Backend = task.BackendCloud
		return nil
	})
}

func (c *cloudBackend) RunTask(ctx context.Context, name string, ts *task.Task, stream func(line string)) (int, error) {
	return c.p.RunTask(ctx, name, ts, stream)
}

func (c *cloudBackend) Exec(ctx context.Context, name, cmd string, stream func(line string)) (int, error) {
	return c.p.Exec(ctx, name, cmd, stream)
}

func (c *cloudBackend) Down(ctx context.Context, name string) error {
	return c.p.Down(ctx, name)
}

func (c *cloudBackend) Stop(ctx context.Context, name string) error {
	return c.p.Stop(ctx, name)
}

func (c *cloudBackend) Start(ctx context.Context, name string) error {
	return c.p.Start(ctx, name)
}

// GpiletHealth reports live node health for the cloud backend.
func (c *cloudBackend) GpiletHealth(ctx context.Context, cluster *state.Cluster, node *state.Node) string {
	return c.p.GpiletHealth(ctx, cluster, node)
}
