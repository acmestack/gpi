package aws

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/acmestack/gpi/internal/cloud"
)

// CloudName is the canonical identifier for this provider, used by Name(),
// Cloud() and the cloud registry.
const CloudName = "aws"

// Provider implements cloud.Provider for AWS EC2. It may be bound to explicit
// credentials via NewProvider; otherwise it loads env/disk creds.
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
		return NewClientWithCreds(p.creds.Region, *p.creds)
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
		spec.Region = client.region
	}
	// Decode the aws config section once; network helpers take it as a param.
	cfg := LoadConfig()

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
		// Reuse running instances: keep the first NumNodes in order.
		reused := running[:spec.NumNodes]
		for i := range reused {
			reused[i].Name = spec.NamePrefix
		}
		return reused, nil
	}
	if len(stopped) > 0 && spec.ResumeStoppedNodes {
		// Restart stopped instances to make up the shortfall.
		need := spec.NumNodes - len(running)
		if len(stopped) > need {
			stopped = stopped[:need]
		}
		ids := make([]string, 0, len(stopped))
		for _, inst := range stopped {
			ids = append(ids, inst.ID)
		}
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

	if spec.ImageID == "" {
		imageID, err := client.GetImage(ctx, "ubuntu")
		if err != nil {
			return nil, err
		}
		spec.ImageID = imageID
	}
	if spec.SecurityGroupID == "" {
		if err := p.vpcFor(ctx, client, cfg, spec); err != nil {
			return nil, err
		}
		if err := p.securityGroupFor(ctx, client, cfg, spec); err != nil {
			return nil, err
		}
	}

	// Collect candidate subnets (default VPC's existing ones, else create a
	// fresh network). Try each in turn; some AZs may lack capacity.
	subnets, err := p.subnetsFor(ctx, client, cfg, spec)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for i, subnet := range subnets {
		launchSpec := *spec
		launchSpec.VSwitchID = subnet.SubnetId
		launchSpec.Zone = subnet.AvailabilityZone
		ids, err := client.RunInstances(ctx, &launchSpec)
		if err == nil {
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
		if awsRetryable(err) && i < len(subnets)-1 {
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

func awsRetryable(err error) bool {
	msg := err.Error()
	for _, sub := range []string{
		"InsufficientInstanceCapacity", "InsufficientCapacity", "InstanceLimitExceeded",
		"Unsupported", "InvalidParameterValue", "IncorrectVpcState", "PendingVerification",
	} {
		if strings.Contains(msg, sub) {
			return true
		}
	}
	return false
}

// vpcFor resolves the VPC to use into spec.VPCID: the caller-specified one, a
// VPC named in config, the account's default VPC, or a freshly created VPC when
// none exist.
func (p Provider) vpcFor(ctx context.Context, client *Client, cfg *Config, spec *cloud.LaunchSpec) error {
	if spec.VPCID != "" {
		return nil
	}
	vpcs, err := client.DescribeVpcs(ctx)
	if err != nil {
		return err
	}
	if cfg != nil {
		if id := matchVPCByName(vpcs, cfg.VPCNames); id != "" {
			spec.VPCID = id
			return nil
		}
	}
	for _, vpc := range vpcs {
		if vpc.IsDefault {
			spec.VPCID = vpc.VpcId
			return nil
		}
	}
	subnets, err := p.createVpcNetwork(ctx, client, spec.Region, "")
	if err != nil {
		return err
	}
	spec.VPCID = subnets[0].VpcId
	return nil
}

// matchVPCByName returns the id of the first VPC whose id or Name tag is in
// want, or "" when none match.
func matchVPCByName(vpcs []vpcItem, want []string) string {
	for _, w := range want {
		for _, vpc := range vpcs {
			if vpc.VpcId == w || vpc.Name() == w {
				return vpc.VpcId
			}
		}
	}
	return ""
}

// securityGroupFor resolves the security group into spec.SecurityGroupID: one
// named in config (by name or id), or a freshly created "gpi-sg" with standard
// rules.
func (p Provider) securityGroupFor(ctx context.Context, client *Client, cfg *Config, spec *cloud.LaunchSpec) error {
	if cfg != nil && cfg.SecurityGroupName != "" {
		groups, err := client.DescribeSecurityGroups(ctx)
		if err != nil {
			return err
		}
		if id := matchSecurityGroupByID(groups, cfg.SecurityGroupName); id != "" {
			spec.SecurityGroupID = id
			return nil
		}
	}
	groupID, err := client.CreateSecurityGroup(ctx, "gpi-sg", "gpi managed security group", spec.VPCID)
	if err != nil {
		return err
	}
	for _, rule := range []struct{ from, to int }{
		{22, 22},
		{1024, 65535},
	} {
		if err := client.AuthorizeSecurityGroup(ctx, groupID, rule.from, rule.to, "tcp"); err != nil {
			return err
		}
	}
	spec.SecurityGroupID = groupID
	return nil
}

// matchSecurityGroupByID returns the id of the first security group whose id
// or name matches want, or "" when none match.
func matchSecurityGroupByID(groups []securityGroupItem, want string) string {
	for _, g := range groups {
		if g.GroupName == want || g.GroupId == want {
			return g.GroupId
		}
	}
	return ""
}

// subnetsFor returns candidate subnets for the launch spec: subnets named in
// config, the default VPC's existing subnets (preferring the requested zone),
// or a freshly created network when none.
func (p Provider) subnetsFor(ctx context.Context, client *Client, cfg *Config, spec *cloud.LaunchSpec) ([]subnetItem, error) {
	vpcID := spec.VPCID
	if vpcID == "" {
		vpcs, err := client.DescribeVpcs(ctx)
		if err != nil {
			return nil, err
		}
		for _, vpc := range vpcs {
			if vpc.IsDefault {
				vpcID = vpc.VpcId
				break
			}
		}
	}
	if vpcID != "" {
		subnets, err := client.DescribeSubnets(ctx, vpcID)
		if err != nil {
			return nil, err
		}
		// Prefer subnets named in config (by Name tag or id).
		if cfg != nil && len(cfg.SubnetNames) > 0 {
			if wanted := matchSubnetsByName(subnets, cfg.SubnetNames); len(wanted) > 0 {
				return wanted, nil
			}
		}
		if len(subnets) > 0 {
			// Prefer the requested zone, then return all as fallbacks.
			if spec.Zone != "" {
				var zoned []subnetItem
				for _, s := range subnets {
					if s.AvailabilityZone == spec.Zone {
						zoned = append(zoned, s)
					}
				}
				if len(zoned) > 0 {
					return zoned, nil
				}
			}
			return subnets, nil
		}
	}
	return p.createVpcNetwork(ctx, client, spec.Region, spec.Zone)
}

// matchSubnetsByName returns the subnets whose id or Name tag is in want, in
// the configured order.
func matchSubnetsByName(subnets []subnetItem, want []string) []subnetItem {
	var out []subnetItem
	for _, w := range want {
		for _, s := range subnets {
			if s.SubnetId == w || s.Name() == w {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// createVpcNetwork creates a VPC + IGW + route table + subnet and returns the
// subnet(s) as a single candidate.
func (p Provider) createVpcNetwork(ctx context.Context, client *Client, region, zone string) ([]subnetItem, error) {
	vpcID, err := client.CreateVpc(ctx, "10.0.0.0/16", "gpi-vpc")
	if err != nil {
		return nil, err
	}
	igwID, err := client.CreateInternetGateway(ctx, "gpi-igw")
	if err != nil {
		return nil, err
	}
	if err := client.AttachInternetGateway(ctx, igwID, vpcID); err != nil {
		return nil, err
	}
	routeTableID, err := client.CreateRouteTable(ctx, vpcID)
	if err != nil {
		return nil, err
	}
	if err := client.CreateRoute(ctx, routeTableID, igwID); err != nil {
		return nil, err
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
	subnetID, err := client.CreateSubnet(ctx, vpcID, zone, "10.0.1.0/24")
	if err != nil {
		return nil, err
	}
	if err := client.AssociateRouteTable(ctx, routeTableID, subnetID); err != nil {
		return nil, err
	}
	return []subnetItem{{SubnetId: subnetID, AvailabilityZone: zone, VpcId: vpcID}}, nil
}

func (p Provider) ListInstances(ctx context.Context, region string, namePrefix string) ([]*cloud.Instance, error) {
	client, err := p.client()
	if err != nil {
		return nil, err
	}
	if region != "" {
		client.region = region
	}
	instances, err := client.DescribeInstances(ctx, nil, namePrefix)
	if err != nil {
		return nil, err
	}
	out := make([]*cloud.Instance, 0, len(instances))
	for i := range instances {
		out = append(out, &instances[i])
	}
	return out, nil
}

func (p Provider) DescribeInstances(ctx context.Context, region string, ids []string) ([]*cloud.Instance, error) {
	client, err := p.client()
	if err != nil {
		return nil, err
	}
	if region != "" {
		client.region = region
	}
	instances, err := client.DescribeInstances(ctx, ids, "")
	if err != nil {
		return nil, err
	}
	out := make([]*cloud.Instance, 0, len(instances))
	for i := range instances {
		out = append(out, &instances[i])
	}
	return out, nil
}

func (p Provider) StopInstances(ctx context.Context, region string, ids []string) error {
	client, err := p.client()
	if err != nil {
		return err
	}
	if region != "" {
		client.region = region
	}
	for _, id := range ids {
		if err := client.StopInstance(ctx, id); err != nil {
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
	if region != "" {
		client.region = region
	}
	for _, id := range ids {
		if err := client.StartInstance(ctx, id); err != nil {
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
	if region != "" {
		client.region = region
	}
	for _, id := range ids {
		if err := client.TerminateInstance(ctx, id); err != nil {
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
	if region != "" {
		client.region = region
	}
	instances, err := client.DescribeInstances(ctx, []string{id}, "")
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
	if region != "" {
		client.region = region
	}
	return client.DescribeZones(ctx, region)
}

func (p Provider) CreateKeyPair(ctx context.Context, region, name string) (string, error) {
	client, err := p.client()
	if err != nil {
		return "", err
	}
	if region != "" {
		client.region = region
	}
	return client.CreateKeyPair(ctx, name)
}

func (p Provider) DeleteKeyPair(ctx context.Context, region, name string) error {
	client, err := p.client()
	if err != nil {
		return err
	}
	if region != "" {
		client.region = region
	}
	return client.DeleteKeyPair(ctx, name)
}

func (p Provider) CreateSecurityGroup(ctx context.Context, region, vpcID, name, desc string) (string, error) {
	client, err := p.client()
	if err != nil {
		return "", err
	}
	if region != "" {
		client.region = region
	}
	return client.CreateSecurityGroup(ctx, name, desc, vpcID)
}

func (p Provider) AuthorizeSecurityGroup(ctx context.Context, region, groupID string, portFrom, portTo int, protocol string) error {
	client, err := p.client()
	if err != nil {
		return err
	}
	if region != "" {
		client.region = region
	}
	return client.AuthorizeSecurityGroup(ctx, groupID, portFrom, portTo, protocol)
}

func (p Provider) CreateVPC(ctx context.Context, region, cidr, name string) (string, error) {
	client, err := p.client()
	if err != nil {
		return "", err
	}
	if region != "" {
		client.region = region
	}
	return client.CreateVpc(ctx, cidr, name)
}

func (p Provider) CreateVSwitch(ctx context.Context, region, vpcID, zone, cidr, name string) (string, error) {
	client, err := p.client()
	if err != nil {
		return "", err
	}
	if region != "" {
		client.region = region
	}
	return client.CreateSubnet(ctx, vpcID, zone, cidr)
}

func (p Provider) ListVSwitches(ctx context.Context, region, vpcID string) ([]cloud.VSwitch, error) {
	client, err := p.client()
	if err != nil {
		return nil, err
	}
	if region != "" {
		client.region = region
	}
	return client.ListVSwitches(ctx, vpcID)
}

func (p Provider) GetImage(ctx context.Context, region, platform string) (string, error) {
	client, err := p.client()
	if err != nil {
		return "", err
	}
	if region != "" {
		client.region = region
	}
	return client.GetImage(ctx, platform)
}

func init() {
	cloud.Register(Provider{})
	cloud.RegisterFactory(CloudName, func(creds *cloud.Credentials) (cloud.Provider, error) {
		return NewProvider(&Credentials{
			AccessKeyID:     creds.AccessKeyID,
			SecretAccessKey: creds.SecretAccessKey,
			Region:          creds.Region,
		}), nil
	})
}
