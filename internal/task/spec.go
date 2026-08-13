package task

// SSHTarget describes an existing host to attach to (backend: existing).
type SSHTarget struct {
	// Host is the SSH host address.
	Host string `yaml:"host" json:"host"`
	// User is the SSH user (default root).
	User string `yaml:"user" json:"user"`
	// Key is the SSH private key path used to connect.
	Key string `yaml:"key" json:"key"`
	// Port is the SSH port (default 22).
	Port int `yaml:"port" json:"port"`
}

// DockerSpec configures a docker execution (backend: docker).
type DockerSpec struct {
	// Image is the container image to run the task in.
	Image string `yaml:"image" json:"image"`
	// Volumes maps host paths to container mount points.
	Volumes map[string]string `yaml:"volumes,omitempty" json:"volumes,omitempty"`
	// Envs are extra environment variables for the container.
	Envs map[string]string `yaml:"envs,omitempty" json:"envs,omitempty"`
	// Gpus is the number of host GPUs to expose to the container.
	Gpus int `yaml:"gpus,omitempty" json:"gpus,omitempty"`
}

// ServiceSpec describes how to expose a task as a replicated service.
type ServiceSpec struct {
	// Replicas is the number of service replicas.
	Replicas int `yaml:"replicas" json:"replicas"`
	// Port is the service port each replica listens on.
	Port int `yaml:"port" json:"port"`
	// HealthCheck is an optional command/path used to probe replica health.
	HealthCheck string `yaml:"health_check,omitempty" json:"healthCheck,omitempty"`
	// WorkingDir is the working directory for the service run command.
	WorkingDir string `yaml:"working_dir,omitempty" json:"workingDir,omitempty"`
	// Run is the service run command (falls back to the task run command).
	Run string `yaml:"run,omitempty" json:"run,omitempty"`
	// ReadyServer is an optional URL that signals replica readiness.
	ReadyServer string `yaml:"ready_server,omitempty" json:"readyServer,omitempty"`
	// Ports lists additional ports to expose on the replica.
	Ports []int `yaml:"ports,omitempty" json:"ports,omitempty"`
	// TargetConcurrentSessions is the desired concurrency for autoscaling.
	TargetConcurrentSessions int `yaml:"target_concurrent_sessions,omitempty" json:"targetConcurrentSessions,omitempty"`
}
