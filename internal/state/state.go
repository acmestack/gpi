package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/acmestack/gpi/internal/optimizer"
)

// Node is a single cloud VM belonging to a Cluster.
type Node struct {
	ID           string `json:"id"`
	PublicIP     string `json:"public_ip"`
	PrivateIP    string `json:"private_ip"`
	InstanceType string `json:"instance_type"`
	Zone         string `json:"zone"`
	Status       string `json:"status"`
	Role         string `json:"role,omitempty"`
}

const (
	RoleHead   = "head"
	RoleWorker = "worker"
)

// ClusterStatus is the lifecycle state of a Cluster.
type ClusterStatus string

const (
	ClusterUp           ClusterStatus = "up"
	ClusterStopped      ClusterStatus = "stopped"
	ClusterError        ClusterStatus = "error"
	ClusterTerminated   ClusterStatus = "terminated"
	ClusterProvisioning ClusterStatus = "provisioning"
	ClusterDown         ClusterStatus = "down"
)

// CloudCreds stores the provider access keys used to provision a cluster, so
// later lifecycle operations (down/stop/start) reuse the same credentials.
type CloudCreds struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	Region          string `json:"region,omitempty"`
}

// SSHTarget records the connection info for an attached existing host
// (backend: existing).
type SSHTarget struct {
	Host string `json:"host"`
	User string `json:"user,omitempty"`
	Key  string `json:"key,omitempty"`
	Port int    `json:"port,omitempty"`
}

// Cluster is the persistent record of a provisioned group of nodes.
type Cluster struct {
	Name          string            `json:"name"`
	Status        ClusterStatus     `json:"status"`
	Backend       string            `json:"backend,omitempty"`
	Cloud         string            `json:"cloud"`
	Region        string            `json:"region"`
	NumNodes      int               `json:"num_nodes"`
	Instances     []Node            `json:"instances"`
	Launch        *optimizer.Launch `json:"launch,omitempty"`
	TaskYAML      string            `json:"task_yaml,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	CloudCreds    *CloudCreds       `json:"cloud_creds,omitempty"`
	SSHTarget     *SSHTarget        `json:"ssh_target,omitempty"`
	KeyName       string            `json:"key_name,omitempty"`
	KeyPath       string            `json:"key_path,omitempty"`
	SecurityGroup string            `json:"security_group_id,omitempty"`
	VPCID         string            `json:"vpc_id,omitempty"`
	VSwitchID     string            `json:"vswitch_id,omitempty"`
	LastError     string            `json:"last_error,omitempty"`
	CreatedAt     int64             `json:"created_at"`
	UpdatedAt     int64             `json:"updated_at"`
}

// GetNodeIP returns the head node's public IP, falling back to the first
// instance with a public IP if no head role is set.
func (c *Cluster) GetNodeIP() string {
	for i := range c.Instances {
		if c.Instances[i].PublicIP != "" {
			if c.Instances[i].Role == RoleHead {
				return c.Instances[i].PublicIP
			}
		}
	}
	for i := range c.Instances {
		if c.Instances[i].PublicIP != "" {
			return c.Instances[i].PublicIP
		}
	}
	return ""
}

// Head returns the head node, or the first instance if none is marked as head.
func (c *Cluster) Head() *Node {
	for i := range c.Instances {
		if c.Instances[i].Role == RoleHead || i == 0 {
			return &c.Instances[i]
		}
	}
	return nil
}

// Workers returns the worker nodes of the cluster.
func (c *Cluster) Workers() []*Node {
	var out []*Node
	for i := range c.Instances {
		if c.Instances[i].Role == RoleWorker {
			out = append(out, &c.Instances[i])
		}
	}
	return out
}

// NodeIPs returns the public IPs of every instance in the cluster.
func (c *Cluster) NodeIPs() []string {
	out := make([]string, 0, len(c.Instances))
	for i := range c.Instances {
		out = append(out, c.Instances[i].PublicIP)
	}
	return out
}

func (c *Cluster) createdAt() int64 { return c.CreatedAt }
func (c *Cluster) updatedAt() int64 { return c.UpdatedAt }

// Store is the persistent, in-memory-cached state manager. Reads and writes go
// through a Backend (file, sqlite or mysql) selected by configuration.
type Store struct {
	mu             sync.RWMutex
	backend        Backend
	clusters       map[string]*Cluster
	services       map[string]*Service
	jobs           map[string]*Job
	clusterYAMLs   map[string]*ClusterYAML
	clusterHistory map[string]*ClusterHistory
	clusterEvents  []*ClusterEvent
	config         map[string]*ConfigEntry
	tokens         []*ServiceAccountToken
}

// DefaultDir returns the state directory, honoring GPI_HOME and falling back
// to ~/.gpi.
func DefaultDir() (string, error) {
	if env := os.Getenv("GPI_HOME"); env != "" {
		return env, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gpi"), nil
}

func (s *Store) save() error {
	if err := s.backend.SaveClusters(s.clusters); err != nil {
		return err
	}
	if err := s.backend.SaveServices(s.services); err != nil {
		return err
	}
	if err := s.backend.SaveJobs(s.jobs); err != nil {
		return err
	}
	if err := s.backend.SaveClusterYAMLs(s.clusterYAMLs); err != nil {
		return err
	}
	if err := s.backend.SaveClusterHistory(s.clusterHistory); err != nil {
		return err
	}
	if err := s.backend.SaveClusterEvents(s.clusterEvents); err != nil {
		return err
	}
	if err := s.backend.SaveConfig(s.config); err != nil {
		return err
	}
	return s.backend.SaveTokens(s.tokens)
}

// Close releases backend resources (e.g. SQL connections).
func (s *Store) Close() error {
	return s.backend.Close()
}

// AddCluster stores a new cluster with timestamps and persists the change.
func (s *Store) AddCluster(c *Cluster) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.clusters[c.Name]; exists {
		return fmt.Errorf("cluster %s already exists", c.Name)
	}
	now := time.Now().Unix()
	c.CreatedAt = now
	c.UpdatedAt = now
	if c.Status == "" {
		c.Status = ClusterProvisioning
	}
	s.clusters[c.Name] = c
	return s.save()
}

// GetCluster returns the named cluster, or an error if it does not exist.
func (s *Store) GetCluster(name string) (*Cluster, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.clusters[name]
	if !ok {
		return nil, errors.New("cluster not found")
	}
	return c, nil
}

// ListClusters returns all clusters sorted by name.
func (s *Store) ListClusters() []*Cluster {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.clusters))
	for name := range s.clusters {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]*Cluster, 0, len(names))
	for _, name := range names {
		out = append(out, s.clusters[name])
	}
	return out
}

// UpdateCluster applies fn to the named cluster and persists the change.
func (s *Store) UpdateCluster(name string, fn func(*Cluster) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clusters[name]
	if !ok {
		return errors.New("cluster not found")
	}
	if err := fn(c); err != nil {
		return err
	}
	c.UpdatedAt = time.Now().Unix()
	return s.save()
}

// DeleteCluster removes the named cluster and persists the change.
func (s *Store) DeleteCluster(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.clusters[name]; !ok {
		return errors.New("cluster not found")
	}
	delete(s.clusters, name)
	return s.save()
}
