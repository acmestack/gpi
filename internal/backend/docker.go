package backend

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/acmestack/gpi/internal/optimizer"
	"github.com/acmestack/gpi/internal/state"
	"github.com/acmestack/gpi/internal/task"
)

// dockerBackend runs the task's setup/run inside a local Docker container.
// The container acts as the "cluster" (single node, ID = container name).
type dockerBackend struct {
	store *state.Store
}

func newDockerBackend(store *state.Store) *dockerBackend {
	return &dockerBackend{store: store}
}

func (d *dockerBackend) Name() string { return task.BackendDocker }

func (d *dockerBackend) Launch(ctx context.Context, name string, ts *task.Task, _ *optimizer.Launch) (*state.Cluster, error) {
	image := ts.Docker.Image
	logger.Info("docker launch", "cluster", name, "image", image, "backend", task.BackendDocker)
	args := []string{"run", "-d", "--name", name}
	for k, v := range ts.Docker.Volumes {
		args = append(args, "-v", k+":"+v)
	}
	for k, v := range ts.Docker.Envs {
		args = append(args, "-e", k+"="+v)
	}
	for k, v := range ts.Envs {
		args = append(args, "-e", k+"="+v)
	}
	if ts.Docker.Gpus > 0 {
		args = append(args, "--gpus", "all")
	}
	args = append(args, "--entrypoint", "sleep", image, "infinity")

	// Remove any stale container with the same name first.
	exec.Command("docker", "rm", "-f", name).Run()

	if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("docker run: %s", strings.TrimSpace(string(out)))
	}

	cluster := &state.Cluster{
		Name:     name,
		Status:   state.ClusterUp,
		Backend:  task.BackendDocker,
		Cloud:    "docker",
		NumNodes: 1,
		TaskYAML: ts.String(),
		Instances: []state.Node{{
			ID:       "docker-" + name,
			Status:   "running",
			PublicIP: "localhost",
		}},
	}
	if err := d.store.AddCluster(cluster); err != nil {
		return nil, err
	}
	return cluster, nil
}

func (d *dockerBackend) RunTask(ctx context.Context, name string, ts *task.Task, stream func(line string)) (int, error) {
	if ts.Workdir != "" {
		if stream != nil {
			stream("(docker) workdir sync not supported")
		}
	}
	if ts.Setup != "" {
		if code, err := d.exec(ctx, name, ts.Setup, stream); err != nil {
			return -1, err
		} else if code != 0 {
			return code, nil
		}
	}
	if ts.Run != "" {
		return d.exec(ctx, name, ts.Run, stream)
	}
	return 0, nil
}

func (d *dockerBackend) Exec(ctx context.Context, name, cmd string, stream func(line string)) (int, error) {
	return d.exec(ctx, name, cmd, stream)
}

// Down removes the container and its state record.
func (d *dockerBackend) Down(ctx context.Context, name string) error {
	logger.Info("docker teardown", "cluster", name)
	exec.Command("docker", "rm", "-f", name).Run()
	return d.store.DeleteCluster(name)
}

// Stop stops the container (data retained).
func (d *dockerBackend) Stop(_ context.Context, name string) error {
	logger.Info("docker stop", "cluster", name)
	out, err := exec.Command("docker", "stop", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker stop: %s", strings.TrimSpace(string(out)))
	}
	return d.store.UpdateCluster(name, func(c *state.Cluster) error {
		c.Status = state.ClusterStopped
		return nil
	})
}

// Start resumes a stopped container.
func (d *dockerBackend) Start(_ context.Context, name string) error {
	logger.Info("docker start", "cluster", name)
	out, err := exec.Command("docker", "start", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker start: %s", strings.TrimSpace(string(out)))
	}
	return d.store.UpdateCluster(name, func(c *state.Cluster) error {
		c.Status = state.ClusterUp
		return nil
	})
}

func (d *dockerBackend) exec(ctx context.Context, name, script string, stream func(string)) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "exec", name, "bash", "-c", script)
	cmd.Stdout = dockerWriter(stream)
	cmd.Stderr = dockerWriter(stream)
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return -1, err
	}
	return 0, nil
}

func dockerWriter(stream func(string)) interface{ Write([]byte) (int, error) } {
	return &lineWriter{stream: stream}
}

// lineWriter splits output on newlines and forwards each line to the stream.
type lineWriter struct {
	stream func(string)
	buf    string
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf += string(p)
	for {
		idx := strings.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimRight(w.buf[:idx], "\r")
		w.buf = w.buf[idx+1:]
		if w.stream != nil && line != "" {
			w.stream(line)
		}
	}
	return len(p), nil
}
