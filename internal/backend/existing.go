package backend

import (
	"context"
	"errors"
	"fmt"

	"github.com/acmestack/gpi/internal/optimizer"
	"github.com/acmestack/gpi/internal/state"
	"github.com/acmestack/gpi/internal/task"
)

// existingBackend attaches to an already-running host via SSH without
// provisioning anything. setup/run are executed over SSH on that host.
type existingBackend struct {
	store *state.Store
}

func newExistingBackend(store *state.Store) *existingBackend {
	return &existingBackend{store: store}
}

func (e *existingBackend) Name() string { return task.BackendExisting }

func (e *existingBackend) Launch(ctx context.Context, name string, ts *task.Task, _ *optimizer.Launch) (*state.Cluster, error) {
	target := &state.SSHTarget{
		Host: ts.SSH.Host,
		User: ts.SSH.User,
		Key:  ts.SSH.Key,
		Port: ts.SSH.Port,
	}
	if target.User == "" {
		target.User = "root"
	}
	cluster := &state.Cluster{
		Name:      name,
		Status:    state.ClusterUp,
		Backend:   task.BackendExisting,
		Cloud:     "existing",
		NumNodes:  1,
		TaskYAML:  ts.String(),
		SSHTarget: target,
		Instances: []state.Node{{
			ID:       "ssh-" + target.Host,
			PublicIP: target.Host,
			Status:   "running",
		}},
	}
	if err := e.store.AddCluster(cluster); err != nil {
		return nil, err
	}
	return cluster, nil
}

func (e *existingBackend) RunTask(ctx context.Context, name string, ts *task.Task, stream func(line string)) (int, error) {
	target, err := e.target(name)
	if err != nil {
		return -1, err
	}
	if ts.Workdir != "" {
		// TODO: rsync workdir to the existing host like the cloud backend.
		if stream != nil {
			stream("(existing) workdir sync not supported; run from current dir")
		}
	}
	if ts.Setup != "" {
		if _, code, err := sshRun(ctx, target, ts.Setup, stream); err != nil {
			return -1, err
		} else if code != 0 {
			return code, nil
		}
	}
	if ts.Run != "" {
		return sshCode(ctx, target, ts.Run, stream)
	}
	return 0, nil
}

func (e *existingBackend) Exec(ctx context.Context, name, cmd string, stream func(line string)) (int, error) {
	target, err := e.target(name)
	if err != nil {
		return -1, err
	}
	return sshCode(ctx, target, cmd, stream)
}

// Down just removes the state record; the external host is left untouched.
func (e *existingBackend) Down(ctx context.Context, name string) error {
	return e.store.DeleteCluster(name)
}

// Stop/Start are not applicable to an externally managed host.
func (e *existingBackend) Stop(_ context.Context, _ string) error {
	return errors.New("stop is not supported for the existing backend")
}

func (e *existingBackend) Start(_ context.Context, _ string) error {
	return errors.New("start is not supported for the existing backend")
}

func (e *existingBackend) target(name string) (*state.SSHTarget, error) {
	cluster, err := e.store.GetCluster(name)
	if err != nil {
		return nil, err
	}
	if cluster.SSHTarget == nil {
		return nil, fmt.Errorf("cluster %s has no SSH target", name)
	}
	return cluster.SSHTarget, nil
}

func sshCode(ctx context.Context, target *state.SSHTarget, script string, stream func(string)) (int, error) {
	_, code, err := sshRun(ctx, target, script, stream)
	return code, err
}
