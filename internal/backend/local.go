package backend

import (
	"context"
	"errors"
	"os/exec"

	"github.com/acmestack/gpi/internal/optimizer"
	"github.com/acmestack/gpi/internal/state"
	"github.com/acmestack/gpi/internal/task"
)

// localBackend runs the task's setup/run directly on the control-plane machine
// (no provisioning, no SSH). Useful for quick local execution and testing.
type localBackend struct {
	store *state.Store
}

func newLocalBackend(store *state.Store) *localBackend {
	return &localBackend{store: store}
}

func (l *localBackend) Name() string { return task.BackendLocal }

func (l *localBackend) Launch(ctx context.Context, name string, ts *task.Task, _ *optimizer.Launch) (*state.Cluster, error) {
	cluster := &state.Cluster{
		Name:      name,
		Status:    state.ClusterUp,
		Backend:   task.BackendLocal,
		Cloud:     "local",
		NumNodes:  1,
		TaskYAML:  ts.String(),
		Instances: []state.Node{{ID: "local", Status: "running", PublicIP: "localhost"}},
	}
	if err := l.store.AddCluster(cluster); err != nil {
		return nil, err
	}
	return cluster, nil
}

func (l *localBackend) RunTask(ctx context.Context, name string, ts *task.Task, stream func(line string)) (int, error) {
	if ts.Workdir != "" {
		if stream != nil {
			stream("(local) workdir sync not supported; run from current dir")
		}
	}
	if ts.Setup != "" {
		if code, err := localExec(ctx, ts.Setup, stream); err != nil {
			return -1, err
		} else if code != 0 {
			return code, nil
		}
	}
	if ts.Run != "" {
		return localExec(ctx, ts.Run, stream)
	}
	return 0, nil
}

func (l *localBackend) Exec(ctx context.Context, name, cmd string, stream func(line string)) (int, error) {
	return localExec(ctx, cmd, stream)
}

// Down just removes the state record (nothing to tear down locally).
func (l *localBackend) Down(_ context.Context, name string) error {
	return l.store.DeleteCluster(name)
}

func (l *localBackend) Stop(_ context.Context, _ string) error {
	return errors.New("stop is not supported for the local backend")
}

func (l *localBackend) Start(_ context.Context, _ string) error {
	return errors.New("start is not supported for the local backend")
}

func localExec(ctx context.Context, script string, stream func(string)) (int, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", script)
	cmd.Stdout = &lineWriter{stream: stream}
	cmd.Stderr = &lineWriter{stream: stream}
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return -1, err
	}
	return 0, nil
}
