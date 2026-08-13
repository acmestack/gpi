package task

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Task is the parsed representation of a task YAML file: what to launch,
// how to prepare the nodes, and what command to run.
type Task struct {
	// Name is the cluster/task name (defaults to a generated name if empty).
	Name string `yaml:"name" json:"name"`
	// NumNodes is the number of nodes to provision; >1 auto-forms a Ray
	// head/worker cluster (default 1).
	NumNodes int `yaml:"num_nodes" json:"numNodes"`
	// Resources declares the compute requirements (cloud/region/instance
	// type/cpus/memory/accelerators/...); optional ordered failover list.
	Resources *Resources `yaml:"resources,omitempty" json:"resources,omitempty"`
	// Workdir is the local working directory uploaded to every node.
	Workdir string `yaml:"workdir,omitempty" json:"workdir,omitempty"`
	// FileMounts maps local files to remote paths copied to each node.
	FileMounts map[string]string `yaml:"file_mounts,omitempty" json:"fileMounts,omitempty"`
	// Tags are key-value labels attached to the cloud instances.
	Tags map[string]string `yaml:"tags,omitempty" json:"tags,omitempty"`
	// Credentials optionally overrides cloud access keys for this task
	// (provider-agnostic map; falls back to env/disk when absent).
	Credentials *Credentials `yaml:"credentials,omitempty" json:"credentials,omitempty"`
	// Backend selects the execution backend: cloud | existing | docker | local
	// (default cloud).
	Backend string `yaml:"backend,omitempty" json:"backend,omitempty"`
	// SSH configures the existing host to attach to (backend: existing).
	SSH *SSHTarget `yaml:"ssh,omitempty" json:"ssh,omitempty"`
	// Docker configures container execution (backend: docker).
	Docker *DockerSpec `yaml:"docker,omitempty" json:"docker,omitempty"`
	// Setup is a shell script run once on every node before the task.
	Setup string `yaml:"setup,omitempty" json:"setup,omitempty"`
	// Run is the main command executed on the head node.
	Run string `yaml:"run,omitempty" json:"run,omitempty"`
	// Envs are extra environment variables injected into the task run.
	Envs map[string]string `yaml:"envs,omitempty" json:"envs,omitempty"`
	// Service exposes the task as a replicated service (SkyServe analog).
	Service *ServiceSpec `yaml:"service,omitempty" json:"service,omitempty"`
}

// Backend names for the execution backend abstraction.
const (
	// BackendCloud provisions cloud VMs (default).
	BackendCloud = "cloud"
	// BackendExisting attaches to an already-running host via SSH.
	BackendExisting = "existing"
	// BackendDocker runs the task in a local Docker container.
	BackendDocker = "docker"
	// BackendLocal runs the task directly on the local machine.
	BackendLocal = "local"
)

// ValidBackends lists all supported execution backends.
var ValidBackends = []string{BackendCloud, BackendExisting, BackendDocker, BackendLocal}

// Load reads and parses a task YAML file from disk.
func Load(path string) (*Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read task file: %w", err)
	}
	return Parse(data)
}

// Parse parses task YAML bytes and applies defaults/validation.
func Parse(data []byte) (*Task, error) {
	t := &Task{}
	if err := yaml.Unmarshal(data, t); err != nil {
		return nil, fmt.Errorf("parse task yaml: %w", err)
	}
	if err := t.SetDefaults(); err != nil {
		return nil, err
	}
	return t, nil
}

// SetDefaults fills in default task fields and validates the task.
func (t *Task) SetDefaults() error {
	if t.Name == "" {
		t.Name = "task"
	}
	if t.NumNodes == 0 {
		t.NumNodes = 1
	}
	if t.NumNodes < 1 {
		return errors.New("num_nodes must be >= 1")
	}
	if t.Backend == "" {
		t.Backend = BackendCloud
	}
	if !t.backendValid() {
		return fmt.Errorf("unknown backend %q (want one of %s)", t.Backend, strings.Join(ValidBackends, ", "))
	}
	if err := t.validateBackendConfig(); err != nil {
		return err
	}
	if t.Resources == nil {
		t.Resources = DefaultResources()
	}
	if err := t.Resources.Validate(); err != nil {
		return err
	}
	if t.Service != nil {
		if t.Service.Replicas == 0 {
			t.Service.Replicas = 1
		}
		if t.Service.Port == 0 && len(t.Service.Ports) == 0 {
			return errors.New("service requires at least one port")
		}
		if t.Service.Run == "" && t.Run == "" {
			return errors.New("service requires a run command")
		}
	}
	if err := t.Credentials.Validate(); err != nil {
		return err
	}
	return nil
}

func (t *Task) backendValid() bool {
	for _, b := range ValidBackends {
		if t.Backend == b {
			return true
		}
	}
	return false
}

func (t *Task) validateBackendConfig() error {
	switch t.Backend {
	case BackendExisting:
		if t.SSH == nil || t.SSH.Host == "" {
			return errors.New("backend \"existing\" requires an ssh: block with host")
		}
	case BackendDocker:
		if t.Docker == nil || t.Docker.Image == "" {
			return errors.New("backend \"docker\" requires a docker: block with image")
		}
	}
	return nil
}

// Command returns the run command, or "" if the task has none.
func (t *Task) Command() string {
	if t == nil || strings.TrimSpace(t.Run) == "" {
		return ""
	}
	return t.Run
}

// OrderFields returns the task field names in canonical display order,
// including only fields set to non-default values.
func (t *Task) OrderFields() []string {
	fields := []string{"name", "resources"}
	for _, f := range []string{"num_nodes", "file_mounts", "workdir", "tags", "credentials", "backend", "ssh", "docker", "setup", "run", "envs", "time", "service"} {
		if reflectNonDefault(t, f) {
			fields = append(fields, f)
		}
	}
	return fields
}

func reflectNonDefault(t *Task, fieldName string) bool {
	switch fieldName {
	case "num_nodes":
		return t.NumNodes > 0
	case "file_mounts":
		return len(t.FileMounts) > 0
	case "workdir":
		return t.Workdir != ""
	case "tags":
		return len(t.Tags) > 0
	case "credentials":
		return t.Credentials != nil
	case "backend":
		return t.Backend != "" && t.Backend != BackendCloud
	case "ssh":
		return t.SSH != nil
	case "docker":
		return t.Docker != nil
	case "setup":
		return t.Setup != ""
	case "run":
		return t.Run != ""
	case "envs":
		return len(t.Envs) > 0
	case "service":
		return t.Service != nil
	}
	return false
}

// String renders the task as a human-readable YAML-like summary.
func (t *Task) String() string {
	lines := []string{fmt.Sprintf("name: %s", t.Name)}
	if t.Resources != nil {
		lines = append(lines, "resources: "+t.Resources.String())
	}
	if t.NumNodes > 1 {
		lines = append(lines, fmt.Sprintf("num_nodes: %d", t.NumNodes))
	}
	if len(t.Tags) > 0 {
		lines = append(lines, "tags: "+mapString(t.Tags))
	}
	if t.Credentials != nil {
		lines = append(lines, "credentials: <provided>")
	}
	if t.Backend != "" && t.Backend != BackendCloud {
		lines = append(lines, "backend: "+t.Backend)
	}
	if t.SSH != nil {
		lines = append(lines, fmt.Sprintf("ssh: %s", t.SSH.Host))
	}
	if t.Docker != nil {
		lines = append(lines, "docker: "+t.Docker.Image)
	}
	if t.Setup != "" {
		lines = append(lines, "setup: <script>")
	}
	if t.Run != "" {
		lines = append(lines, "run: <command>")
	}
	return strings.Join(lines, "\n")
}

// EnvSlice flattens an env map into sorted "KEY=value" strings for shell use.
func EnvSlice(envs map[string]string) []string {
	if len(envs) == 0 {
		return nil
	}
	keys := make([]string, 0, len(envs))
	for k := range envs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+envs[k])
	}
	return out
}

func mapString(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, ",")
}
