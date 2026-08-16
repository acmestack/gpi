package kubernetes

import (
	"time"

	"github.com/acmestack/gpi/internal/config"
)

// Config holds Kubernetes launch preferences read from the gpi config's
// "kubernetes" section (user-level config.yaml and/or project .gpi.yaml).
type Config struct {
	// Context overrides the kubeconfig context used for pods when the task
	// does not pin a region. Empty = the current kubeconfig context.
	Context string `yaml:"context"`
	// Namespace is the K8s namespace where node pods are created (default "default").
	Namespace string `yaml:"namespace"`
	// Image overrides the default gpi-base container image used for node pods
	// (the image must contain gpilet and Ray; see Dockerfile.gpi-base).
	Image string `yaml:"image"`
	// GpiletDir is the directory where gpilet writes its status file.
	GpiletDir string `yaml:"gpilet_dir"`
	// GpiletInterval is the gpilet serve interval in seconds.
	GpiletInterval int `yaml:"gpilet_interval"`
	// RayHeadPort is the Ray GCS (head) port.
	RayHeadPort int `yaml:"ray_head_port"`
	// RayDashboardPort is the Ray dashboard port.
	RayDashboardPort int `yaml:"ray_dashboard_port"`
	// PodWaitTimeout is the per-attempt timeout (seconds) to wait for a node
	// pod to reach Running (e.g. image pull / container start).
	PodWaitTimeout int `yaml:"pod_wait_timeout"`
	// PodWaitRetries is how many attempts (each up to PodWaitTimeout) to wait
	// for a node pod to reach Running before RunInstances fails.
	PodWaitRetries int `yaml:"pod_wait_retries"`
}

// LoadConfig returns the merged "kubernetes" config section, or nil when unset.
func LoadConfig() *Config {
	var c Config
	if err := config.Load().Section(cloudName, &c); err != nil {
		return nil
	}
	return &c
}

// EffectiveNamespace returns the configured namespace or the default.
func (c *Config) EffectiveNamespace() string {
	if c == nil || c.Namespace == "" {
		return defaultNamespace
	}
	return c.Namespace
}

// EffectiveGpiletDir returns the configured gpilet directory or the default.
func (c *Config) EffectiveGpiletDir() string {
	if c == nil || c.GpiletDir == "" {
		return gpiletDir
	}
	return c.GpiletDir
}

// EffectiveGpiletInterval returns the configured gpilet interval or the default.
func (c *Config) EffectiveGpiletInterval() int {
	if c == nil || c.GpiletInterval <= 0 {
		return gpiletIntervalSec
	}
	return c.GpiletInterval
}

// EffectiveRayHeadPort returns the configured Ray head port or the default.
func (c *Config) EffectiveRayHeadPort() int {
	if c == nil || c.RayHeadPort <= 0 {
		return rayHeadPort
	}
	return c.RayHeadPort
}

// EffectiveRayDashboardPort returns the configured Ray dashboard port or the default.
func (c *Config) EffectiveRayDashboardPort() int {
	if c == nil || c.RayDashboardPort <= 0 {
		return rayDashboardPort
	}
	return c.RayDashboardPort
}

// EffectivePodWaitTimeout returns the configured per-attempt pod wait timeout
// or the default.
func (c *Config) EffectivePodWaitTimeout() time.Duration {
	if c == nil || c.PodWaitTimeout <= 0 {
		return podWaitTimeout
	}
	return time.Duration(c.PodWaitTimeout) * time.Second
}

// EffectivePodWaitRetries returns the configured pod wait retry count or the
// default.
func (c *Config) EffectivePodWaitRetries() int {
	if c == nil || c.PodWaitRetries <= 0 {
		return podWaitRetries
	}
	return c.PodWaitRetries
}
