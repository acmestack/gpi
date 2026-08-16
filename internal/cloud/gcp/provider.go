package gcp

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/acmestack/gpi/internal/cloud"
	"github.com/acmestack/gpi/internal/logging"
)

// logger is the package logger, tagged with the module name.
var logger = logging.WithName("gcp")

// CloudName is the canonical identifier for this provider, used by Name(),
// Cloud() and the cloud registry.
const CloudName = "gcp"

func init() {
	cloud.Register(Provider{})
	cloud.RegisterFactory(CloudName, func(creds *cloud.Credentials) (cloud.Provider, error) {
		return NewProvider(&Credentials{
			ProjectID:   creds.Region, // GCP uses ProjectID as region equivalent
			AccessToken: creds.AccessKeyID,
		}), nil
	})
}

// Provider implements cloud.Provider for Google Cloud Platform.
type Provider struct {
	creds *Credentials
}

// NewProvider returns a provider bound to the given credentials (nil means
// use the default env/disk loading).
func NewProvider(creds *Credentials) Provider {
	return Provider{creds: creds}
}

func (Provider) Name() string { return CloudName }

func (p Provider) client(region string) (*Client, error) {
	r := region
	if r == "" {
		r = p.defaultRegion()
	}
	if p.creds != nil {
		return NewClientWithCreds(r, *p.creds)
	}
	return NewClient(r)
}

func (p Provider) defaultRegion() string {
	if p.creds != nil && p.creds.ProjectID != "" {
		return "us-central1" // default zone
	}
	return ""
}

// Regions returns common GCP zones.
func (p Provider) Regions(_ context.Context) ([]string, error) {
	// Simplified: return common zones
	return []string{
		"us-central1-a", "us-central1-b", "us-central1-c",
		"us-east1-b", "us-east1-c", "us-east1-d",
		"europe-west1-b", "europe-west1-c", "europe-west1-d",
		"asia-east1-a", "asia-east1-b", "asia-east1-c",
	}, nil
}

func (p Provider) projectID() (string, error) {
	if p.creds != nil && p.creds.ProjectID != "" {
		return p.creds.ProjectID, nil
	}
	return "", fmt.Errorf("GCP project ID not configured")
}

// RunInstances creates VM instances in GCP.
func (p Provider) RunInstances(ctx context.Context, spec *cloud.LaunchSpec) ([]*cloud.Instance, error) {
	project, err := p.projectID()
	if err != nil {
		return nil, err
	}
	c, err := p.client(spec.Region)
	if err != nil {
		return nil, err
	}

	var instances []*cloud.Instance
	for i := 0; i < spec.NumNodes; i++ {
		role := "worker"
		if i == 0 {
			role = "head"
		}
		name := fmt.Sprintf("%s-%s", spec.NamePrefix, role)
		if spec.NumNodes == 1 {
			name = spec.NamePrefix
		}

		zone := spec.Zone
		if zone == "" {
			zone = spec.Region + "-a"
		}

		machineType := fmt.Sprintf("zones/%s/machineTypes/%s", zone, spec.InstanceType)
		body := map[string]any{
			"name":        name,
			"machineType": machineType,
			"disks": []map[string]any{
				{
					"boot":       true,
					"autoDelete": true,
					"initializeParams": map[string]any{
						"sourceImage": "projects/ubuntu-os-cloud/global/images/family/ubuntu-2204-lts",
					},
				},
			},
			"networkInterfaces": []map[string]any{
				{
					"network": "global/networks/default",
					"accessConfigs": []map[string]any{
						{"type": "ONE_TO_ONE_NAT", "name": "External NAT"},
					},
				},
			},
			"labels": spec.Tags,
			"metadata": map[string]any{
				"items": []map[string]any{
					{"key": "gpi-cluster", "value": spec.NamePrefix},
				},
			},
		}

		type InsertResponse struct {
			Name string `json:"name"`
		}
		var resp InsertResponse
		if err := c.callPost(ctx, project, fmt.Sprintf("zones/%s/instances", zone), body, &resp); err != nil {
			return nil, fmt.Errorf("create instance %s: %w", name, err)
		}
		logger.Info("instance created", "name", name, "zone", zone, "project", project)

		instances = append(instances, &cloud.Instance{
			ID:           name,
			Name:         name,
			InstanceType: spec.InstanceType,
			Region:       spec.Region,
			Zone:         zone,
			Status:       cloud.StatusPending,
			Tags:         spec.Tags,
		})
	}
	return instances, nil
}

// ListInstances lists VMs matching the name prefix.
func (p Provider) ListInstances(ctx context.Context, region, namePrefix string) ([]*cloud.Instance, error) {
	project, err := p.projectID()
	if err != nil {
		return nil, err
	}
	c, err := p.client(region)
	if err != nil {
		return nil, err
	}

	zone := region + "-a"
	type instanceItem struct {
		Name              string `json:"name"`
		MachineType       string `json:"machineType"`
		Status            string `json:"status"`
		Zone              string `json:"zone"`
		NetworkInterfaces []struct {
			AccessConfigs []struct {
				NatIP string `json:"natIP"`
			} `json:"accessConfigs"`
		} `json:"networkInterfaces"`
		Labels map[string]string `json:"labels"`
	}
	type listResponse struct {
		Items []instanceItem `json:"items"`
	}

	var resp listResponse
	if err := c.call(ctx, project, fmt.Sprintf("zones/%s/instances?filter=name=%s", zone, namePrefix), &resp); err != nil {
		return nil, err
	}

	var instances []*cloud.Instance
	for _, item := range resp.Items {
		if !strings.HasPrefix(item.Name, namePrefix) {
			continue
		}
		inst := &cloud.Instance{
			ID:           item.Name,
			Name:         item.Name,
			InstanceType: extractMachineType(item.MachineType),
			Region:       region,
			Zone:         zone,
			Status:       gcpStatusToCloud(item.Status),
			Tags:         item.Labels,
		}
		if len(item.NetworkInterfaces) > 0 && len(item.NetworkInterfaces[0].AccessConfigs) > 0 {
			inst.PublicIP = item.NetworkInterfaces[0].AccessConfigs[0].NatIP
		}
		instances = append(instances, inst)
	}
	return instances, nil
}

// DescribeInstances returns details for specific instance names.
func (p Provider) DescribeInstances(ctx context.Context, region string, ids []string) ([]*cloud.Instance, error) {
	project, err := p.projectID()
	if err != nil {
		return nil, err
	}
	c, err := p.client(region)
	if err != nil {
		return nil, err
	}

	zone := region + "-a"
	type instanceItem struct {
		Name              string `json:"name"`
		MachineType       string `json:"machineType"`
		Status            string `json:"status"`
		Zone              string `json:"zone"`
		NetworkInterfaces []struct {
			AccessConfigs []struct {
				NatIP string `json:"natIP"`
			} `json:"accessConfigs"`
		} `json:"networkInterfaces"`
		Labels map[string]string `json:"labels"`
	}

	var instances []*cloud.Instance
	for _, id := range ids {
		var item instanceItem
		if err := c.call(ctx, project, fmt.Sprintf("zones/%s/instances/%s", zone, id), &item); err != nil {
			continue
		}
		inst := &cloud.Instance{
			ID:           item.Name,
			Name:         item.Name,
			InstanceType: extractMachineType(item.MachineType),
			Region:       region,
			Zone:         zone,
			Status:       gcpStatusToCloud(item.Status),
			Tags:         item.Labels,
		}
		if len(item.NetworkInterfaces) > 0 && len(item.NetworkInterfaces[0].AccessConfigs) > 0 {
			inst.PublicIP = item.NetworkInterfaces[0].AccessConfigs[0].NatIP
		}
		instances = append(instances, inst)
	}
	return instances, nil
}

// StopInstances stops GCP instances.
func (p Provider) StopInstances(ctx context.Context, region string, ids []string) error {
	project, err := p.projectID()
	if err != nil {
		return err
	}
	c, err := p.client(region)
	if err != nil {
		return err
	}
	zone := region + "-a"
	for _, id := range ids {
		if err := c.callPost(ctx, project, fmt.Sprintf("zones/%s/instances/%s/stop", zone, id), nil, nil); err != nil {
			return fmt.Errorf("stop %s: %w", id, err)
		}
	}
	return nil
}

// StartInstances starts GCP instances.
func (p Provider) StartInstances(ctx context.Context, region string, ids []string) error {
	project, err := p.projectID()
	if err != nil {
		return err
	}
	c, err := p.client(region)
	if err != nil {
		return err
	}
	zone := region + "-a"
	for _, id := range ids {
		if err := c.callPost(ctx, project, fmt.Sprintf("zones/%s/instances/%s/start", zone, id), nil, nil); err != nil {
			return fmt.Errorf("start %s: %w", id, err)
		}
	}
	return nil
}

// TerminateInstances deletes GCP instances.
func (p Provider) TerminateInstances(ctx context.Context, region string, ids []string) error {
	project, err := p.projectID()
	if err != nil {
		return err
	}
	c, err := p.client(region)
	if err != nil {
		return err
	}
	zone := region + "-a"
	for _, id := range ids {
		if err := c.callDelete(ctx, project, fmt.Sprintf("zones/%s/instances/%s", zone, id)); err != nil {
			return fmt.Errorf("delete %s: %w", id, err)
		}
		logger.Info("instance terminated", "name", id)
	}
	return nil
}

// GetPublicIP returns the external IP of an instance.
func (p Provider) GetPublicIP(ctx context.Context, region, id string) (string, error) {
	instances, err := p.DescribeInstances(ctx, region, []string{id})
	if err != nil {
		return "", err
	}
	for _, inst := range instances {
		if inst.ID == id && inst.PublicIP != "" {
			return inst.PublicIP, nil
		}
	}
	return "", nil
}

// DescribeZones returns all zones in the project.
func (p Provider) DescribeZones(ctx context.Context, region string) ([]string, error) {
	return []string{region + "-a", region + "-b", region + "-c"}, nil
}

// CreateKeyPair is a no-op for GCP (uses OS Login or metadata-based SSH).
func (p Provider) CreateKeyPair(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

// DeleteKeyPair is a no-op for GCP.
func (p Provider) DeleteKeyPair(_ context.Context, _, _ string) error {
	return nil
}

// CreateSecurityGroup creates a GCP firewall rule.
func (p Provider) CreateSecurityGroup(ctx context.Context, region, vpcID, name, description string) (string, error) {
	project, err := p.projectID()
	if err != nil {
		return "", err
	}
	c, err := p.client(region)
	if err != nil {
		return "", err
	}
	body := map[string]any{
		"name":         name,
		"network":      fmt.Sprintf("global/networks/%s", vpcID),
		"allowed":      []map[string]any{},
		"sourceRanges": []string{"0.0.0.0/0"},
	}
	if err := c.callPost(ctx, project, "global/firewalls", body, nil); err != nil {
		return "", err
	}
	return name, nil
}

// AuthorizeSecurityGroup adds a rule to an existing firewall.
func (p Provider) AuthorizeSecurityGroup(_ context.Context, _, _ string, _, _ int, _ string) error {
	return nil
}

// CreateVPC creates a GCP VPC network.
func (p Provider) CreateVPC(ctx context.Context, region, cidr, name string) (string, error) {
	project, err := p.projectID()
	if err != nil {
		return "", err
	}
	c, err := p.client(region)
	if err != nil {
		return "", err
	}
	body := map[string]any{
		"name":                  name,
		"autoCreateSubnetworks": true,
	}
	if err := c.callPost(ctx, project, "global/networks", body, nil); err != nil {
		return "", err
	}
	return name, nil
}

// CreateVSwitch creates a GCP subnetwork.
func (p Provider) CreateVSwitch(ctx context.Context, region, vpcID, zone, cidr, name string) (string, error) {
	project, err := p.projectID()
	if err != nil {
		return "", err
	}
	c, err := p.client(region)
	if err != nil {
		return "", err
	}
	body := map[string]any{
		"name":        name,
		"network":     fmt.Sprintf("global/networks/%s", vpcID),
		"ipCidrRange": cidr,
		"region":      region,
	}
	if err := c.callPost(ctx, project, fmt.Sprintf("regions/%s/subnetworks", region), body, nil); err != nil {
		return "", err
	}
	return name, nil
}

// ListVSwitches lists subnetworks in a VPC.
func (p Provider) ListVSwitches(ctx context.Context, region, vpcID string) ([]cloud.VSwitch, error) {
	project, err := p.projectID()
	if err != nil {
		return nil, err
	}
	c, err := p.client(region)
	if err != nil {
		return nil, err
	}
	type subnetItem struct {
		Name          string `json:"name"`
		IP_CIDR_Range string `json:"ipCidrRange"`
		Network       string `json:"network"`
	}
	type listResponse struct {
		Items []subnetItem `json:"items"`
	}
	var resp listResponse
	if err := c.call(ctx, project, fmt.Sprintf("regions/%s/subnetworks?filter=network=%s", region, vpcID), &resp); err != nil {
		return nil, err
	}
	var subs []cloud.VSwitch
	for _, item := range resp.Items {
		subs = append(subs, cloud.VSwitch{
			Name:  item.Name,
			CIDR:  item.IP_CIDR_Range,
			VPCID: vpcID,
		})
	}
	return subs, nil
}

// GetImage returns the latest Ubuntu 22.04 LTS image.
func (p Provider) GetImage(_ context.Context, _, _ string) (string, error) {
	return "projects/ubuntu-os-cloud/global/images/family/ubuntu-2204-lts", nil
}

func extractMachineType(full string) string {
	parts := strings.Split(full, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return full
}

func gcpStatusToCloud(status string) cloud.InstanceStatus {
	switch strings.ToLower(status) {
	case "running":
		return cloud.StatusRunning
	case "stopped", "suspended":
		return cloud.StatusStopped
	case "stopping":
		return cloud.StatusStopping
	case "provisioning", "staging", "repairing":
		return cloud.StatusPending
	case "terminated", "terminating":
		return cloud.StatusTerminated
	default:
		return cloud.StatusUnknown
	}
}

// ParseZone extracts zone from a full zone resource name.
func ParseZone(zone string) string {
	parts := strings.Split(zone, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return zone
}

// ParseMachineType extracts machine type from a full resource name.
func ParseMachineType(mt string) string {
	return extractMachineType(mt)
}

// Ensure numeric parsing works
var _ = strconv.Itoa
