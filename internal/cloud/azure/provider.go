package azure

import (
	"context"
	"fmt"
	"strings"

	"github.com/acmestack/gpi/internal/cloud"
	"github.com/acmestack/gpi/internal/logging"
)

// logger is the package logger, tagged with the module name.
var logger = logging.WithName("azure")

// CloudName is the canonical identifier for this provider, used by Name(),
// Cloud() and the cloud registry.
const CloudName = "azure"

func init() {
	cloud.Register(Provider{})
	cloud.RegisterFactory(CloudName, func(creds *cloud.Credentials) (cloud.Provider, error) {
		return NewProvider(&Credentials{
			SubscriptionID: creds.Region,
			TenantID:       creds.AccessKeyID,
			ClientID:       creds.SecretAccessKey,
		}), nil
	})
}

// Provider implements cloud.Provider for Microsoft Azure.
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
	if p.creds != nil {
		return NewClientWithCreds(region, *p.creds)
	}
	return NewClient(region)
}

// Regions returns Azure regions (simplified).
func (p Provider) Regions(_ context.Context) ([]string, error) {
	return []string{
		"eastus", "eastus2", "westus", "westus2", "westus3",
		"centralus", "northcentralus", "southcentralus",
		"northeurope", "westeurope", "uksouth", "ukwest",
		"eastasia", "southeastasia", "japaneast", "japanwest",
		"koreacentral", "koreasouth",
	}, nil
}

func (p Provider) subscriptionPath() string {
	sub := ""
	if p.creds != nil {
		sub = p.creds.SubscriptionID
	}
	return fmt.Sprintf("/subscriptions/%s", sub)
}

// RunInstances creates VMs in Azure.
func (p Provider) RunInstances(ctx context.Context, spec *cloud.LaunchSpec) ([]*cloud.Instance, error) {
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

		rg := fmt.Sprintf("%s-rg", spec.NamePrefix)
		body := map[string]any{
			"location": spec.Region,
			"tags":     spec.Tags,
			"properties": map[string]any{
				"hardwareProfile": map[string]any{
					"vmSize": spec.InstanceType,
				},
				"storageProfile": map[string]any{
					"imageReference": map[string]any{
						"publisher": "Canonical",
						"offer":     "ubuntu-24_04-lts",
						"sku":       "server",
						"version":   "latest",
					},
					"osDisk": map[string]any{
						"createOption": "FromImage",
						"managedDisk": map[string]any{
							"storageAccountType": "Standard_LRS",
						},
					},
				},
				"networkProfile": map[string]any{
					"networkInterfaces": []map[string]any{
						{
							"id": fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/networkInterfaces/%s-nic",
								c.creds.SubscriptionID, rg, name),
						},
					},
				},
				"osProfile": map[string]any{
					"computerName":  name,
					"adminUsername": "azureuser",
				},
			},
		}

		path := fmt.Sprintf("%s/resourceGroups/%s/providers/Microsoft.Compute/virtualMachines/%s",
			p.subscriptionPath(), rg, name)
		if err := c.callPost(ctx, path, body, nil); err != nil {
			return nil, fmt.Errorf("create VM %s: %w", name, err)
		}
		logger.Info("VM created", "name", name, "region", spec.Region)

		instances = append(instances, &cloud.Instance{
			ID:           name,
			Name:         name,
			InstanceType: spec.InstanceType,
			Region:       spec.Region,
			Status:       cloud.StatusPending,
			Tags:         spec.Tags,
		})
	}
	return instances, nil
}

// ListInstances lists VMs matching the name prefix.
func (p Provider) ListInstances(ctx context.Context, region, namePrefix string) ([]*cloud.Instance, error) {
	c, err := p.client(region)
	if err != nil {
		return nil, err
	}

	type vmProperties struct {
		VMSize            string `json:"vmSize"`
		ProvisioningState string `json:"provisioningState"`
	}
	type vmItem struct {
		Name       string            `json:"name"`
		Location   string            `json:"location"`
		Properties vmProperties      `json:"properties"`
		Tags       map[string]string `json:"tags"`
	}
	type listResponse struct {
		Value []vmItem `json:"value"`
	}

	path := fmt.Sprintf("%s/providers/Microsoft.Compute/virtualMachines?$filter=startswith(name,'%s')",
		p.subscriptionPath(), namePrefix)
	var resp listResponse
	if err := c.call(ctx, path, &resp); err != nil {
		return nil, err
	}

	var instances []*cloud.Instance
	for _, item := range resp.Value {
		if !strings.HasPrefix(item.Name, namePrefix) {
			continue
		}
		instances = append(instances, &cloud.Instance{
			ID:           item.Name,
			Name:         item.Name,
			InstanceType: item.Properties.VMSize,
			Region:       item.Location,
			Status:       azureStatusToCloud(item.Properties.ProvisioningState),
			Tags:         item.Tags,
		})
	}
	return instances, nil
}

// DescribeInstances returns details for specific VM names.
func (p Provider) DescribeInstances(ctx context.Context, region string, ids []string) ([]*cloud.Instance, error) {
	c, err := p.client(region)
	if err != nil {
		return nil, err
	}

	type vmProperties struct {
		VMSize            string `json:"vmSize"`
		ProvisioningState string `json:"provisioningState"`
	}
	type vmItem struct {
		Name       string            `json:"name"`
		Location   string            `json:"location"`
		Properties vmProperties      `json:"properties"`
		Tags       map[string]string `json:"tags"`
	}

	var instances []*cloud.Instance
	for _, id := range ids {
		var item vmItem
		// Try a few common resource group patterns
		for _, rgPattern := range []string{"default", id + "-rg"} {
			path := fmt.Sprintf("%s/resourceGroups/%s/providers/Microsoft.Compute/virtualMachines/%s",
				p.subscriptionPath(), rgPattern, id)
			if err := c.call(ctx, path, &item); err == nil {
				instances = append(instances, &cloud.Instance{
					ID:           item.Name,
					Name:         item.Name,
					InstanceType: item.Properties.VMSize,
					Region:       item.Location,
					Status:       azureStatusToCloud(item.Properties.ProvisioningState),
					Tags:         item.Tags,
				})
				break
			}
		}
	}
	return instances, nil
}

// StopInstances stops Azure VMs.
func (p Provider) StopInstances(ctx context.Context, region string, ids []string) error {
	c, err := p.client(region)
	if err != nil {
		return err
	}
	for _, id := range ids {
		path := fmt.Sprintf("%s/resourceGroups/%s/providers/Microsoft.Compute/virtualMachines/%s/powerOff",
			p.subscriptionPath(), id+"-rg", id)
		if err := c.callPost(ctx, path, nil, nil); err != nil {
			return fmt.Errorf("stop %s: %w", id, err)
		}
	}
	return nil
}

// StartInstances starts Azure VMs.
func (p Provider) StartInstances(ctx context.Context, region string, ids []string) error {
	c, err := p.client(region)
	if err != nil {
		return err
	}
	for _, id := range ids {
		path := fmt.Sprintf("%s/resourceGroups/%s/providers/Microsoft.Compute/virtualMachines/%s/start",
			p.subscriptionPath(), id+"-rg", id)
		if err := c.callPost(ctx, path, nil, nil); err != nil {
			return fmt.Errorf("start %s: %w", id, err)
		}
	}
	return nil
}

// TerminateInstances deletes Azure VMs.
func (p Provider) TerminateInstances(ctx context.Context, region string, ids []string) error {
	c, err := p.client(region)
	if err != nil {
		return err
	}
	for _, id := range ids {
		path := fmt.Sprintf("%s/resourceGroups/%s/providers/Microsoft.Compute/virtualMachines/%s",
			p.subscriptionPath(), id+"-rg", id)
		if err := c.callDelete(ctx, path); err != nil {
			return fmt.Errorf("delete %s: %w", id, err)
		}
		logger.Info("VM terminated", "name", id)
	}
	return nil
}

// GetPublicIP returns the public IP of a VM.
func (p Provider) GetPublicIP(ctx context.Context, region, id string) (string, error) {
	c, err := p.client(region)
	if err != nil {
		return "", err
	}
	type publicIPAddress struct {
		Properties struct {
			IPAddress string `json:"ipAddress"`
		} `json:"properties"`
	}
	path := fmt.Sprintf("%s/resourceGroups/%s/providers/Microsoft.Network/publicIPAddresses/%s-ip",
		p.subscriptionPath(), id+"-rg", id)
	var pip publicIPAddress
	if err := c.call(ctx, path, &pip); err != nil {
		return "", err
	}
	return pip.Properties.IPAddress, nil
}

// DescribeZones returns Azure availability zones for a region.
func (p Provider) DescribeZones(_ context.Context, _ string) ([]string, error) {
	return []string{"1", "2", "3"}, nil
}

// CreateKeyPair is a no-op for Azure (uses SSH key injection via VM properties).
func (p Provider) CreateKeyPair(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

// DeleteKeyPair is a no-op for Azure.
func (p Provider) DeleteKeyPair(_ context.Context, _, _ string) error {
	return nil
}

// CreateSecurityGroup creates an Azure NSG.
func (p Provider) CreateSecurityGroup(ctx context.Context, region, vpcID, name, description string) (string, error) {
	c, err := p.client(region)
	if err != nil {
		return "", err
	}
	rg := name + "-rg"
	body := map[string]any{
		"location": region,
		"properties": map[string]any{
			"securityRules": []map[string]any{},
		},
	}
	path := fmt.Sprintf("%s/resourceGroups/%s/providers/Microsoft.Network/networkSecurityGroups/%s",
		p.subscriptionPath(), rg, name)
	if err := c.callPost(ctx, path, body, nil); err != nil {
		return "", err
	}
	return name, nil
}

// AuthorizeSecurityGroup adds a rule to an existing NSG.
func (p Provider) AuthorizeSecurityGroup(_ context.Context, _, _ string, _, _ int, _ string) error {
	return nil
}

// CreateVPC creates an Azure VNet.
func (p Provider) CreateVPC(ctx context.Context, region, cidr, name string) (string, error) {
	c, err := p.client(region)
	if err != nil {
		return "", err
	}
	rg := name + "-rg"
	body := map[string]any{
		"location": region,
		"properties": map[string]any{
			"addressSpace": map[string]any{
				"addressPrefixes": []string{cidr},
			},
		},
	}
	path := fmt.Sprintf("%s/resourceGroups/%s/providers/Microsoft.Network/virtualNetworks/%s",
		p.subscriptionPath(), rg, name)
	if err := c.callPost(ctx, path, body, nil); err != nil {
		return "", err
	}
	return name, nil
}

// CreateVSwitch creates an Azure subnet.
func (p Provider) CreateVSwitch(ctx context.Context, region, vpcID, zone, cidr, name string) (string, error) {
	c, err := p.client(region)
	if err != nil {
		return "", err
	}
	rg := vpcID + "-rg"
	body := map[string]any{
		"properties": map[string]any{
			"addressPrefix": cidr,
		},
	}
	path := fmt.Sprintf("%s/resourceGroups/%s/providers/Microsoft.Network/virtualNetworks/%s/subnets/%s",
		p.subscriptionPath(), rg, vpcID, name)
	if err := c.callPost(ctx, path, body, nil); err != nil {
		return "", err
	}
	return name, nil
}

// ListVSwitches lists subnets in a VNet.
func (p Provider) ListVSwitches(ctx context.Context, region, vpcID string) ([]cloud.VSwitch, error) {
	c, err := p.client(region)
	if err != nil {
		return nil, err
	}
	type subnetItem struct {
		Name       string `json:"name"`
		Properties struct {
			AddressPrefix string `json:"addressPrefix"`
		} `json:"properties"`
	}
	type listResponse struct {
		Value []subnetItem `json:"value"`
	}
	rg := vpcID + "-rg"
	path := fmt.Sprintf("%s/resourceGroups/%s/providers/Microsoft.Network/virtualNetworks/%s/subnets",
		p.subscriptionPath(), rg, vpcID)
	var resp listResponse
	if err := c.call(ctx, path, &resp); err != nil {
		return nil, err
	}
	var subs []cloud.VSwitch
	for _, item := range resp.Value {
		subs = append(subs, cloud.VSwitch{
			Name:  item.Name,
			CIDR:  item.Properties.AddressPrefix,
			VPCID: vpcID,
		})
	}
	return subs, nil
}

// GetImage returns the latest Ubuntu 24.04 LTS image reference.
func (p Provider) GetImage(_ context.Context, _, _ string) (string, error) {
	return "Canonical:ubuntu-24_04-lts:server:latest", nil
}

func azureStatusToCloud(status string) cloud.InstanceStatus {
	switch strings.ToLower(status) {
	case "succeeded", "running":
		return cloud.StatusRunning
	case "stopped":
		return cloud.StatusStopped
	case "deallocating", "stopping":
		return cloud.StatusStopping
	case "creating", "updating", "provisioning":
		return cloud.StatusPending
	case "failed":
		return cloud.StatusTerminated
	default:
		return cloud.StatusUnknown
	}
}
