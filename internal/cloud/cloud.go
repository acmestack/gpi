package cloud

import (
	"context"
	"fmt"

	"github.com/acmestack/gpi/internal/cloud/catalog"
)

// InstanceStatus is the lifecycle state of a cloud instance.
type InstanceStatus string

const (
	StatusPending     InstanceStatus = "pending"
	StatusRunning     InstanceStatus = "running"
	StatusStopped     InstanceStatus = "stopped"
	StatusStopping    InstanceStatus = "stopping"
	StatusStarting    InstanceStatus = "starting"
	StatusTerminating InstanceStatus = "terminating"
	StatusTerminated  InstanceStatus = "terminated"
	StatusUnknown     InstanceStatus = "unknown"
)

// Instance describes a cloud VM returned by a Provider.
type Instance struct {
	ID           string
	Name         string
	PublicIP     string
	PrivateIP    string
	Status       InstanceStatus
	InstanceType string
	Region       string
	Zone         string
	CreatedAt    int64
	Tags         map[string]string
}

// LaunchSpec carries everything needed to provision one or more instances.
type LaunchSpec struct {
	InstanceType    string
	Region          string
	Zone            string
	NumNodes        int
	ImageID         string
	KeyName         string
	SecurityGroupID string
	VSwitchID       string
	VPCID           string
	SpotStrategy    string
	SpotPriceLimit  float64
	DiskSizeGiB     int
	UserData        string
	Tags            map[string]string
	NamePrefix      string
	// ResumeStoppedNodes makes RunInstances reuse stopped instances of the
	// cluster (restarting them) instead of always creating new ones, mirroring
	// SkyPilot's reuse of stopped nodes on relaunch.
	ResumeStoppedNodes bool
}

// Provider is the cloud abstraction. Implementations are registered by name
// and are responsible for signing requests against their cloud API.
type Provider interface {
	Name() string
	Regions(ctx context.Context) ([]string, error)
	RunInstances(ctx context.Context, spec *LaunchSpec) ([]*Instance, error)
	ListInstances(ctx context.Context, region string, namePrefix string) ([]*Instance, error)
	DescribeInstances(ctx context.Context, region string, ids []string) ([]*Instance, error)
	StopInstances(ctx context.Context, region string, ids []string) error
	StartInstances(ctx context.Context, region string, ids []string) error
	TerminateInstances(ctx context.Context, region string, ids []string) error
	GetPublicIP(ctx context.Context, region, id string) (string, error)
	DescribeZones(ctx context.Context, region string) ([]string, error)
	CreateKeyPair(ctx context.Context, region, name string) (string, error)
	DeleteKeyPair(ctx context.Context, region, name string) error
	CreateSecurityGroup(ctx context.Context, region, vpcID, name, description string) (string, error)
	AuthorizeSecurityGroup(ctx context.Context, region, groupID string, portFrom, portTo int, protocol string) error
	CreateVPC(ctx context.Context, region, cidr, name string) (string, error)
	CreateVSwitch(ctx context.Context, region, vpcID, zone, cidr, name string) (string, error)
	ListVSwitches(ctx context.Context, region, vpcID string) ([]VSwitch, error)
	GetImage(ctx context.Context, region, platform string) (string, error)
}

// VSwitch is a cloud subnet within a VPC.
type VSwitch struct {
	ID    string
	Name  string
	Zone  string
	CIDR  string
	VPCID string
}

// Credentials carries provider access keys. A nil/empty region means the
// provider should fall back to its default region logic.
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	Region          string
}

var registry = map[string]Provider{}

var factories = map[string]func(*Credentials) (Provider, error){}

// Register registers the default (env/disk credential) provider for a cloud.
// If the provider also implements catalog.Source (instance specs + prices),
// it is automatically registered as the cloud's metadata source, so a new
// cloud only ever writes one struct implementing both capabilities.
func Register(p Provider) {
	registry[p.Name()] = p
	if s, ok := p.(catalog.Source); ok {
		catalog.Register(s)
	}
}

// RegisterFactory registers a constructor that returns a provider using the
// given per-task credentials (may be nil to mean "use default env/disk creds").
func RegisterFactory(name string, f func(*Credentials) (Provider, error)) {
	factories[name] = f
}

// Get returns the default (env/disk credential) provider for a cloud name,
// or nil if the cloud is not registered.
func Get(name string) Provider {
	return registry[name]
}

// New returns a provider for the given cloud. If creds is non-nil and has an
// access key, it returns a provider bound to those credentials; otherwise it
// falls back to the default env/disk credential loading.
func New(name string, creds *Credentials) (Provider, error) {
	if creds != nil && creds.AccessKeyID != "" {
		if f, ok := factories[name]; ok {
			return f(creds)
		}
	}
	p := registry[name]
	if p == nil {
		return nil, fmt.Errorf("cloud provider %q not registered", name)
	}
	return p, nil
}

// Has reports whether a cloud provider is registered.
func Has(name string) bool {
	_, ok := registry[name]
	return ok
}

// Names lists all registered cloud provider names.
func Names() []string {
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	return out
}
