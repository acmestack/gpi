package task

import (
	"errors"
	"fmt"
	"strings"
)

const (
	DiskSmall  = "disk-100"
	DiskMedium = "disk-512"
	DiskLarge  = "disk-1024"
)

// Resources expresses a task's compute requirements: cloud/region pinning,
// instance type, CPU/memory/disk ranges, accelerators, spot preference, and
// node labels for Ray scheduling. TimeSec is the user's estimate of how long
// the task runs (seconds); the "time" optimizer ranks candidates by it.
//
// Ordered is an optional failover list of alternative resource requirements
// (mirrors SkyPilot's resources.ordered): the fields on this Resources act as
// defaults for every entry, and entries override them. The optimizer tries
// entries in the given order — first entry's candidates rank first, then the
// second entry's, and so on.
type Resources struct {
	Cloud        string            `yaml:"cloud" json:"cloud,omitempty"`
	Region       string            `yaml:"region,omitempty" json:"region,omitempty"`
	Zone         string            `yaml:"zone,omitempty" json:"zone,omitempty"`
	InstanceType string            `yaml:"instance_type,omitempty" json:"instanceType,omitempty"`
	Cpus         *Range            `yaml:"cpus,omitempty" json:"cpus,omitempty"`
	Memory       *Range            `yaml:"memory,omitempty" json:"memory,omitempty"`
	DiskSize     *Range            `yaml:"disk_size,omitempty" json:"diskSize,omitempty"`
	Accelerators Accelerators      `yaml:"accelerators,omitempty" json:"accelerators,omitempty"`
	UseSpot      *bool             `yaml:"use_spot,omitempty" json:"useSpot,omitempty"`
	TimeSec      *int              `yaml:"time_sec,omitempty" json:"timeSec,omitempty"`
	Labels       map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	Ordered      []*Resources      `yaml:"ordered,omitempty" json:"ordered,omitempty"`
}

type rawResources struct {
	Cloud        string            `yaml:"cloud" json:"cloud"`
	Region       string            `yaml:"region" json:"region"`
	Zone         string            `yaml:"zone" json:"zone"`
	InstanceType string            `yaml:"instance_type" json:"instanceType"`
	Cpus         *Range            `yaml:"cpus" json:"cpus"`
	Memory       *Range            `yaml:"memory" json:"memory"`
	DiskSize     *Range            `yaml:"disk_size" json:"diskSize"`
	Accelerators any               `yaml:"accelerators" json:"accelerators"`
	UseSpot      *bool             `yaml:"use_spot" json:"useSpot"`
	TimeSec      *int              `yaml:"time_sec" json:"timeSec"`
	Labels       map[string]string `yaml:"labels" json:"labels"`
	Ordered      []*Resources      `yaml:"ordered" json:"ordered"`
}

// UnmarshalYAML parses resource filters, decoding accelerators flexibly.
func (r *Resources) UnmarshalYAML(unmarshal func(any) error) error {
	var raw rawResources
	if err := unmarshal(&raw); err != nil {
		return err
	}
	*r = Resources{
		Cloud:        raw.Cloud,
		Region:       raw.Region,
		Zone:         raw.Zone,
		InstanceType: raw.InstanceType,
		Cpus:         raw.Cpus,
		Memory:       raw.Memory,
		DiskSize:     raw.DiskSize,
		UseSpot:      raw.UseSpot,
		TimeSec:      raw.TimeSec,
		Labels:       raw.Labels,
	}
	if raw.Accelerators != nil {
		accel, err := ParseAccelerators(raw.Accelerators)
		if err != nil {
			return err
		}
		r.Accelerators = accel
	}
	// Ordered failover entries inherit this Resources' fields as defaults;
	// a field set on an entry overrides the inherited default.
	for _, entry := range raw.Ordered {
		entry.fillDefaultsFrom(r)
		r.Ordered = append(r.Ordered, entry)
	}
	return nil
}

// fillDefaultsFrom copies any unset field of r from base (outer defaults).
// Entry fields that are already set win over the defaults.
func (r *Resources) fillDefaultsFrom(base *Resources) {
	if r.Cloud == "" {
		r.Cloud = base.Cloud
	}
	if r.Region == "" {
		r.Region = base.Region
	}
	if r.Zone == "" {
		r.Zone = base.Zone
	}
	if r.InstanceType == "" {
		r.InstanceType = base.InstanceType
	}
	if r.Cpus == nil {
		r.Cpus = base.Cpus
	}
	if r.Memory == nil {
		r.Memory = base.Memory
	}
	if r.DiskSize == nil {
		r.DiskSize = base.DiskSize
	}
	if r.UseSpot == nil {
		r.UseSpot = base.UseSpot
	}
	if r.TimeSec == nil {
		r.TimeSec = base.TimeSec
	}
	if len(r.Accelerators) == 0 {
		r.Accelerators = base.Accelerators
	}
	if len(r.Labels) == 0 {
		r.Labels = base.Labels
	}
}

// DefaultResources returns an empty Resources (all filters unset).
func DefaultResources() *Resources {
	return &Resources{}
}

// Copy returns a deep copy of the resources.
func (r *Resources) Copy() *Resources {
	if r == nil {
		return nil
	}
	c := *r
	if r.Cpus != nil {
		cp := *r.Cpus
		cp.Min = cloneFloat(r.Cpus.Min)
		cp.Max = cloneFloat(r.Cpus.Max)
		c.Cpus = &cp
	}
	if r.Memory != nil {
		cp := *r.Memory
		cp.Min = cloneFloat(r.Memory.Min)
		cp.Max = cloneFloat(r.Memory.Max)
		c.Memory = &cp
	}
	if r.DiskSize != nil {
		cp := *r.DiskSize
		cp.Min = cloneFloat(r.DiskSize.Min)
		cp.Max = cloneFloat(r.DiskSize.Max)
		c.DiskSize = &cp
	}
	if r.UseSpot != nil {
		spot := *r.UseSpot
		c.UseSpot = &spot
	}
	if r.TimeSec != nil {
		ts := *r.TimeSec
		c.TimeSec = &ts
	}
	if r.Accelerators != nil {
		c.Accelerators = make(Accelerators, len(r.Accelerators))
		for k, v := range r.Accelerators {
			c.Accelerators[k] = v
		}
	}
	if r.Labels != nil {
		c.Labels = make(map[string]string, len(r.Labels))
		for k, v := range r.Labels {
			c.Labels[k] = v
		}
	}
	if r.Ordered != nil {
		c.Ordered = make([]*Resources, len(r.Ordered))
		for i, e := range r.Ordered {
			c.Ordered[i] = e.Copy()
		}
	}
	return &c
}

func cloneFloat(f *float64) *float64 {
	if f == nil {
		return nil
	}
	v := *f
	return &v
}

// SetCloud pins the resources to the given cloud and returns the receiver.
func (r *Resources) SetCloud(cloud string) *Resources {
	r.Cloud = cloud
	return r
}

// SetRegion pins the resources to the given region and returns the receiver.
func (r *Resources) SetRegion(region string) *Resources {
	r.Region = region
	return r
}

// SetZone pins the resources to the given zone and returns the receiver.
func (r *Resources) SetZone(zone string) *Resources {
	r.Zone = zone
	return r
}

// SetInstanceType pins the resources to the given instance type and returns
// the receiver.
func (r *Resources) SetInstanceType(it string) *Resources {
	r.InstanceType = it
	return r
}

// Nothing reports whether the resources impose no filters at all.
func (r *Resources) Nothing() bool {
	return r == nil ||
		(r.Cloud == "" && r.Region == "" && r.Zone == "" && r.InstanceType == "" &&
			r.Cpus == nil && r.Memory == nil && r.DiskSize == nil &&
			len(r.Accelerators) == 0 && r.UseSpot == nil && r.TimeSec == nil &&
			len(r.Labels) == 0 && len(r.Ordered) == 0)
}

// Validate checks that the resources are well-formed (e.g. a known cloud).
func (r *Resources) Validate() error {
	if r == nil {
		return errors.New("resources must not be nil")
	}
	if r.Cloud != "" && !validCloudName(r.Cloud) {
		return fmt.Errorf("unknown cloud %q", r.Cloud)
	}
	return nil
}

func validCloudName(name string) bool {
	for _, ch := range name {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return false
		}
	}
	return strings.TrimSpace(name) != ""
}

// String renders the resources as a compact human-readable summary.
func (r *Resources) String() string {
	parts := make([]string, 0, 8)
	if r.InstanceType != "" {
		parts = append(parts, r.InstanceType)
	}
	if len(r.Accelerators) > 0 {
		parts = append(parts, r.Accelerators.String())
	}
	if r.Cpus != nil {
		parts = append(parts, "cpus:"+r.Cpus.String())
	}
	if r.Memory != nil {
		parts = append(parts, "mem:"+r.Memory.String())
	}
	if r.DiskSize != nil {
		parts = append(parts, "disk:"+r.DiskSize.String())
	}
	if r.Cloud != "" {
		parts = append(parts, "cloud:"+r.Cloud)
	}
	if r.Region != "" {
		parts = append(parts, r.Region)
	}
	if r.Zone != "" {
		parts = append(parts, r.Zone)
	}
	if r.UseSpot != nil && *r.UseSpot {
		parts = append(parts, "spot")
	}
	if r.TimeSec != nil {
		parts = append(parts, fmt.Sprintf("time:%ds", *r.TimeSec))
	}
	if len(r.Ordered) > 0 {
		parts = append(parts, fmt.Sprintf("ordered:%d", len(r.Ordered)))
	}
	if len(parts) == 0 {
		return "[default]"
	}
	return strings.Join(parts, ", ")
}
