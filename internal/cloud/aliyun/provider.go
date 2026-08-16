package aliyun

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/acmestack/gpi/internal/cloud"
	"github.com/acmestack/gpi/internal/logging"
)

// logger is the package logger, tagged with the module name.
var logger = logging.WithName("aliyun")

// CloudName is the canonical identifier for this provider, used by Name(),
// Cloud() and the cloud registry.
const CloudName = "aliyun"

func init() {
	cloud.Register(Provider{})
	cloud.RegisterFactory(CloudName, func(creds *cloud.Credentials) (cloud.Provider, error) {
		return NewProvider(&Credentials{
			AccessKeyID:     creds.AccessKeyID,
			AccessKeySecret: creds.SecretAccessKey,
		}), nil
	})
}

// Provider implements cloud.Provider for Alibaba Cloud ECS. It may be bound
// to explicit credentials via NewProvider; otherwise it loads env/disk creds.
type Provider struct {
	creds *Credentials
}

// NewProvider returns a provider bound to the given credentials (nil means
// use the default env/disk loading).
func NewProvider(creds *Credentials) Provider {
	return Provider{creds: creds}
}

func (Provider) Name() string { return CloudName }

func (p Provider) client() (*Client, error) {
	if p.creds != nil {
		return NewClientWithCreds("", *p.creds)
	}
	return NewClient("")
}

func (p Provider) Regions(ctx context.Context) ([]string, error) {
	client, err := p.client()
	if err != nil {
		return nil, err
	}
	return client.DescribeRegions(ctx)
}

func (p Provider) RunInstances(ctx context.Context, spec *cloud.LaunchSpec) ([]*cloud.Instance, error) {
	client, err := p.client()
	if err != nil {
		return nil, err
	}
	if spec.Region == "" {
		spec.Region = "cn-hangzhou"
	}

	// Like SkyPilot, first look up existing instances of this cluster: reuse
	// running ones, restart stopped ones, and only create when none fit.
	existing, err := p.ListInstances(ctx, spec.Region, spec.NamePrefix)
	if err != nil {
		return nil, fmt.Errorf("list existing instances: %w", err)
	}
	var running, stopped []*cloud.Instance
	for _, inst := range existing {
		switch inst.Status {
		case cloud.StatusRunning, cloud.StatusPending, cloud.StatusStarting:
			running = append(running, inst)
		case cloud.StatusStopped, cloud.StatusStopping:
			stopped = append(stopped, inst)
		}
	}
	if len(running) >= spec.NumNodes {
		reused := running[:spec.NumNodes]
		for i := range reused {
			reused[i].Name = spec.NamePrefix
		}
		logger.Info("aliyun reuse running instances", "cluster", spec.NamePrefix, "region", spec.Region, "count", len(reused))
		return reused, nil
	}
	if len(stopped) > 0 && spec.ResumeStoppedNodes {
		need := spec.NumNodes - len(running)
		if len(stopped) > need {
			stopped = stopped[:need]
		}
		ids := make([]string, 0, len(stopped))
		for _, inst := range stopped {
			ids = append(ids, inst.ID)
		}
		logger.Info("aliyun resume stopped instances", "cluster", spec.NamePrefix, "region", spec.Region, "count", len(ids))
		if err := p.StartInstances(ctx, spec.Region, ids); err != nil {
			return nil, fmt.Errorf("start stopped instances: %w", err)
		}
		all := append(running, stopped...)
		reused := all[:spec.NumNodes]
		for i := range reused {
			reused[i].Name = spec.NamePrefix
			reused[i].Status = cloud.StatusStarting
		}
		return reused, nil
	}

	// Decode the aliyun config section once; helpers take it as a param.
	cfg := LoadConfig()

	// Resolve the default VPC up-front so both the security group and vswitch
	// reuse the account's existing network (avoids fresh-VPC timing errors).
	if spec.VPCID == "" {
		if cfg != nil && cfg.VPCID != "" {
			spec.VPCID = cfg.VPCID
		} else {
			vpcs, err := client.DescribeVpcs(ctx, spec.Region, true)
			if err != nil {
				return nil, err
			}
			if len(vpcs) > 0 {
				spec.VPCID = vpcs[0].VpcId
			}
		}
	}
	if spec.ImageID == "" {
		imageID, err := client.GetImage(ctx, spec.Region, "ubuntu")
		if err != nil {
			return nil, err
		}
		spec.ImageID = imageID
	}
	if spec.SecurityGroupID == "" {
		if cfg != nil && cfg.SecurityGroupID != "" {
			spec.SecurityGroupID = cfg.SecurityGroupID
		} else {
			groupID, err := p.ensureSecurityGroup(ctx, client, spec.Region, spec.VPCID)
			if err != nil {
				return nil, err
			}
			spec.SecurityGroupID = groupID
		}
	}

	// Collect candidate vswitches (default VPC's existing ones, else create a
	// fresh one). Try each in turn; some zones may be out of stock.
	vswitches, err := p.vswitchesFor(ctx, client, cfg, spec)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for i, vs := range vswitches {
		launchSpec := *spec
		launchSpec.VSwitchID = vs.ID
		if launchSpec.Zone == "" {
			launchSpec.Zone = vs.Zone
		}
		ids, err := client.RunInstances(ctx, &launchSpec)
		if err == nil {
			logger.Info("aliyun create instances", "cluster", spec.NamePrefix, "region", spec.Region, "zone", launchSpec.Zone, "instance", spec.InstanceType, "count", len(ids))
			instances := make([]*cloud.Instance, 0, len(ids))
			for _, id := range ids {
				instances = append(instances, &cloud.Instance{
					ID:           id,
					Name:         spec.NamePrefix,
					InstanceType: spec.InstanceType,
					Region:       spec.Region,
					Zone:         launchSpec.Zone,
					Status:       cloud.StatusPending,
				})
			}
			return instances, nil
		}
		lastErr = err
		// Transient/zone-availability errors: try the next vswitch.
		if isRetryableLaunchError(err) && i < len(vswitches)-1 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(2 * time.Second):
			}
			continue
		}
		return nil, err
	}
	return nil, lastErr
}

// vswitchesFor returns candidate vswitches for the launch spec: vswitches
// configured by id, the default VPC's existing ones, or a freshly created
// vswitch when none exist.
func (p Provider) vswitchesFor(ctx context.Context, client *Client, cfg *Config, spec *cloud.LaunchSpec) ([]cloud.VSwitch, error) {
	region, vpcID, zone := spec.Region, spec.VPCID, spec.Zone
	if cfg != nil && len(cfg.VSwitchIDs) > 0 {
		var out []cloud.VSwitch
		for _, id := range cfg.VSwitchIDs {
			vswitches, err := client.ListVSwitches(ctx, region, vpcID)
			if err != nil {
				return nil, err
			}
			for _, vs := range vswitches {
				if vs.ID == id {
					out = append(out, vs)
					break
				}
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	if vpcID == "" {
		vpcs, err := client.DescribeVpcs(ctx, region, true)
		if err != nil {
			return nil, err
		}
		if len(vpcs) > 0 {
			vpcID = vpcs[0].VpcId
		}
	}
	if vpcID != "" {
		existing, err := client.ListVSwitches(ctx, region, vpcID)
		if err != nil {
			return nil, err
		}
		if len(existing) > 0 {
			return existing, nil
		}
	}
	if vpcID == "" {
		created, err := client.CreateVpc(ctx, region, "172.16.0.0/16", "gpi-vpc")
		if err != nil {
			return nil, err
		}
		vpcID = created
	}
	if zone == "" {
		zones, err := client.DescribeZones(ctx, region)
		if err != nil {
			return nil, err
		}
		if len(zones) == 0 {
			return nil, fmt.Errorf("no zones available in %s", region)
		}
		zone = zones[len(zones)/2]
	}
	id, err := client.CreateVSwitch(ctx, region, vpcID, zone, "172.16.0.0/24", "gpi-vswitch")
	if err != nil {
		return nil, err
	}
	return []cloud.VSwitch{{ID: id, VPCID: vpcID, Zone: zone}}, nil
}

func isRetryableLaunchError(err error) bool {
	msg := err.Error()
	for _, sub := range []string{"NoStock", "InvalidResourceType.NotSupported", "IncorrectVpcStatus", "InvalidVSwitch", "Zone.NotOnSale"} {
		if strings.Contains(msg, sub) {
			return true
		}
	}
	return false
}

func (p Provider) ListInstances(ctx context.Context, region string, namePrefix string) ([]*cloud.Instance, error) {
	client, err := p.client()
	if err != nil {
		return nil, err
	}
	var out []*cloud.Instance
	page := 1
	for {
		batch, err := client.DescribeInstances(ctx, region, nil, namePrefix, page)
		if err != nil {
			return nil, err
		}
		for i := range batch {
			out = append(out, &batch[i])
		}
		if len(batch) < 100 {
			break
		}
		page++
	}
	return out, nil
}

func (p Provider) DescribeInstances(ctx context.Context, region string, ids []string) ([]*cloud.Instance, error) {
	client, err := p.client()
	if err != nil {
		return nil, err
	}
	batch, err := client.DescribeInstances(ctx, region, ids, "", 1)
	if err != nil {
		return nil, err
	}
	out := make([]*cloud.Instance, 0, len(batch))
	for i := range batch {
		out = append(out, &batch[i])
	}
	return out, nil
}

func (p Provider) StopInstances(ctx context.Context, region string, ids []string) error {
	client, err := p.client()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := client.StopInstance(ctx, region, id); err != nil {
			return err
		}
	}
	return nil
}

func (p Provider) StartInstances(ctx context.Context, region string, ids []string) error {
	client, err := p.client()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := client.StartInstance(ctx, region, id); err != nil {
			return err
		}
	}
	return nil
}

func (p Provider) TerminateInstances(ctx context.Context, region string, ids []string) error {
	client, err := p.client()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := client.DeleteInstance(ctx, region, id); err != nil {
			return err
		}
	}
	return nil
}

func (p Provider) GetPublicIP(ctx context.Context, region, id string) (string, error) {
	client, err := p.client()
	if err != nil {
		return "", err
	}
	instances, err := client.DescribeInstances(ctx, region, []string{id}, "", 1)
	if err != nil {
		return "", err
	}
	if len(instances) == 0 {
		return "", fmt.Errorf("instance %s not found", id)
	}
	return instances[0].PublicIP, nil
}

func (p Provider) DescribeZones(ctx context.Context, region string) ([]string, error) {
	client, err := p.client()
	if err != nil {
		return nil, err
	}
	if region == "" {
		region = "cn-hangzhou"
	}
	return client.DescribeZones(ctx, region)
}

func (p Provider) CreateKeyPair(ctx context.Context, region, name string) (string, error) {
	client, err := p.client()
	if err != nil {
		return "", err
	}
	return client.CreateKeyPair(ctx, region, name)
}

func (p Provider) DeleteKeyPair(ctx context.Context, region, name string) error {
	client, err := p.client()
	if err != nil {
		return err
	}
	return client.DeleteKeyPair(ctx, region, name)
}

func (p Provider) CreateSecurityGroup(ctx context.Context, region, vpcID, name, desc string) (string, error) {
	client, err := p.client()
	if err != nil {
		return "", err
	}
	return client.CreateSecurityGroup(ctx, region, vpcID, name, desc)
}

func (p Provider) AuthorizeSecurityGroup(ctx context.Context, region, groupID string, portFrom, portTo int, protocol string) error {
	client, err := p.client()
	if err != nil {
		return err
	}
	return client.AuthorizeSecurityGroup(ctx, region, groupID, portFrom, portTo, protocol)
}

func (p Provider) CreateVPC(ctx context.Context, region, cidr, name string) (string, error) {
	client, err := p.client()
	if err != nil {
		return "", err
	}
	if region == "" {
		region = "cn-hangzhou"
	}
	return client.CreateVpc(ctx, region, cidr, name)
}

func (p Provider) CreateVSwitch(ctx context.Context, region, vpcID, zone, cidr, name string) (string, error) {
	client, err := p.client()
	if err != nil {
		return "", err
	}
	return client.CreateVSwitch(ctx, region, vpcID, zone, cidr, name)
}

func (p Provider) ListVSwitches(ctx context.Context, region, vpcID string) ([]cloud.VSwitch, error) {
	client, err := p.client()
	if err != nil {
		return nil, err
	}
	vswitches, err := client.ListVSwitches(ctx, region, vpcID)
	if err != nil {
		return nil, err
	}
	return vswitches, nil
}

func (p Provider) GetImage(ctx context.Context, region, platform string) (string, error) {
	client, err := p.client()
	if err != nil {
		return "", err
	}
	if region == "" {
		region = "cn-hangzhou"
	}
	return client.GetImage(ctx, region, platform)
}

func (p Provider) ensureSecurityGroup(ctx context.Context, client *Client, region, vpcID string) (string, error) {
	groupID, err := client.CreateSecurityGroup(ctx, region, vpcID, "gpi-sg", "gpi managed security group")
	if err != nil {
		return "", err
	}
	for _, rule := range []struct{ from, to int }{
		{22, 22},
		{1024, 65535},
	} {
		if err := client.AuthorizeSecurityGroup(ctx, region, groupID, rule.from, rule.to, "tcp"); err != nil {
			return "", err
		}
	}
	return groupID, nil
}
