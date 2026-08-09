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
	Name        string            `yaml:"name" json:"name"`
	NumNodes    int               `yaml:"num_nodes" json:"num_nodes"`
	Resources   *Resources        `yaml:"resources" json:"resources"`
	Workdir     string            `yaml:"workdir" json:"workdir"`
	FileMounts  map[string]string `yaml:"file_mounts" json:"file_mounts"`
	Tags        map[string]string `yaml:"tags" json:"tags"`
	Credentials *Credentials      `yaml:"credentials" json:"credentials"`
	Backend     string            `yaml:"backend" json:"backend"`
	SSH         *SSHTarget        `yaml:"ssh,omitempty" json:"ssh,omitempty"`
	Docker      *DockerSpec       `yaml:"docker,omitempty" json:"docker,omitempty"`
	Setup       string            `yaml:"setup" json:"setup"`
	Run         string            `yaml:"run" json:"run"`
	Envs        map[string]string `yaml:"envs" json:"envs"`
	Time        string            `yaml:"time" json:"time"`
	Service     *ServiceSpec      `yaml:"service" json:"service"`
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

// SSHTarget describes an existing host to attach to (backend: existing).
type SSHTarget struct {
	Host string `yaml:"host" json:"host"`
	User string `yaml:"user" json:"user"`
	Key  string `yaml:"key" json:"key"`
	Port int    `yaml:"port" json:"port"`
}

// DockerSpec configures a docker execution (backend: docker).
type DockerSpec struct {
	Image   string            `yaml:"image" json:"image"`
	Volumes map[string]string `yaml:"volumes" json:"volumes"`
	Envs    map[string]string `yaml:"envs" json:"envs"`
	Gpus    int               `yaml:"gpus" json:"gpus"`
}

// Credentials holds optional per-task cloud access keys. If provided they are
// used for this task's launch; otherwise gpi falls back to the default
// env/disk credential loading.
type Credentials struct {
	AWS    *AWSCreds    `yaml:"aws,omitempty" json:"aws,omitempty"`
	Aliyun *AliyunCreds `yaml:"aliyun,omitempty" json:"aliyun,omitempty"`
}

// AWSCreds holds per-task AWS access keys.
type AWSCreds struct {
	AccessKeyID     string `yaml:"access_key_id" json:"access_key_id"`
	SecretAccessKey string `yaml:"secret_access_key" json:"secret_access_key"`
	Region          string `yaml:"region" json:"region"`
}

// AliyunCreds holds per-task Alibaba Cloud access keys.
type AliyunCreds struct {
	AccessKeyID     string `yaml:"access_key_id" json:"access_key_id"`
	AccessKeySecret string `yaml:"access_key_secret" json:"access_key_secret"`
}

// CloudCredentials is a provider-agnostic view used by the provisioner.
type CloudCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	Region          string
}

// ServiceSpec describes how to expose a task as a replicated service.
type ServiceSpec struct {
	Replicas                 int    `yaml:"replicas" json:"replicas"`
	Port                     int    `yaml:"port" json:"port"`
	HealthCheck              string `yaml:"health_check" json:"health_check"`
	WorkingDir               string `yaml:"working_dir" json:"working_dir"`
	Run                      string `yaml:"run" json:"run"`
	ReadyServer              string `yaml:"ready_server" json:"ready_server"`
	Ports                    []int  `yaml:"ports" json:"ports"`
	TargetConcurrentSessions int    `yaml:"target_concurrent_sessions" json:"target_concurrent_sessions"`
}

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

// Validate checks that any supplied credential block is complete (access key
// plus secret). A nil Credentials is valid (falls back to default loading).
func (c *Credentials) Validate() error {
	if c == nil {
		return nil
	}
	if c.AWS != nil {
		if c.AWS.AccessKeyID == "" || c.AWS.SecretAccessKey == "" {
			return errors.New("aws credentials require access_key_id and secret_access_key")
		}
	}
	if c.Aliyun != nil {
		if c.Aliyun.AccessKeyID == "" || c.Aliyun.AccessKeySecret == "" {
			return errors.New("aliyun credentials require access_key_id and access_key_secret")
		}
	}
	return nil
}

// ForCloud returns the cloud-level credentials for the given provider name,
// or nil if no matching credential block was supplied.
func (c *Credentials) ForCloud(name string) *CloudCredentials {
	if c == nil {
		return nil
	}
	switch name {
	case "aws":
		if c.AWS == nil {
			return nil
		}
		return &CloudCredentials{
			AccessKeyID:     c.AWS.AccessKeyID,
			SecretAccessKey: c.AWS.SecretAccessKey,
			Region:          c.AWS.Region,
		}
	case "aliyun":
		if c.Aliyun == nil {
			return nil
		}
		return &CloudCredentials{
			AccessKeyID:     c.Aliyun.AccessKeyID,
			SecretAccessKey: c.Aliyun.AccessKeySecret,
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
	case "time":
		return t.Time != ""
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
