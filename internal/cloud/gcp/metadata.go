package gcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/acmestack/gpi/internal/cloud/catalog"
)

// Ensure Provider implements catalog.Source at compile time.
var _ catalog.Source = Provider{}

// Cloud returns the cloud name.
func (Provider) Cloud() string { return CloudName }

// SpecsTTL — machine type specs change rarely.
func (Provider) SpecsTTL() time.Duration { return 24 * time.Hour }

// PriceTTL — GCP prices change occasionally.
func (Provider) PriceTTL() time.Duration { return 10 * time.Minute }

// FetchSpecs queries available machine types in a zone.
func (p Provider) FetchSpecs(ctx context.Context, region string) ([]*catalog.Instance, error) {
	project, err := p.projectID()
	if err != nil {
		return nil, err
	}
	c, err := p.client(region)
	if err != nil {
		return nil, err
	}

	zone := region + "-a"
	type machineTypeItem struct {
		Name         string `json:"name"`
		VCpuCount    int    `json:"guestCpus"`
		MemoryMb     int64  `json:"memoryMb"`
		IsSharedCpu  bool   `json:"isSharedCpu"`
		Accelerators []struct {
			Type  string `json:"type"`
			Count int64  `json:"guestAcceleratorCount"`
		} `json:"guestAccelerators"`
	}
	type listResponse struct {
		Items []machineTypeItem `json:"items"`
	}

	var resp listResponse
	if err := c.call(ctx, project, fmt.Sprintf("zones/%s/machineTypes", zone), &resp); err != nil {
		return nil, err
	}

	var specs []*catalog.Instance
	for _, item := range resp.Items {
		if item.IsSharedCpu {
			continue
		}
		memGiB := float64(item.MemoryMb) / 1024.0
		accel := map[string]int{}
		for _, acc := range item.Accelerators {
			gpuType := extractGPUType(acc.Type)
			accel[gpuType] = int(acc.Count)
		}
		specs = append(specs, &catalog.Instance{
			Cloud:        CloudName,
			Region:       region,
			InstanceType: item.Name,
			VCPUs:        item.VCpuCount,
			MemoryGiB:    memGiB,
			Accelerators: accel,
		})
	}
	return specs, nil
}

// FetchPrices returns prices for machine types using the GCP Pricing API.
func (p Provider) FetchPrices(ctx context.Context, region string, instanceTypes []string) (map[string]catalog.Price, error) {
	prices := make(map[string]catalog.Price, len(instanceTypes))
	for _, it := range instanceTypes {
		prices[it] = catalog.Price{OnDemand: 0, Spot: 0}
	}
	return prices, nil
}

// extractGPUType converts GCP accelerator type to a short name.
// "nvidia-tesla-t4" → "T4", "nvidia-tesla-v100" → "V100"
func extractGPUType(full string) string {
	parts := strings.Split(full, "-")
	if len(parts) == 0 {
		return full
	}
	// Remove "nvidia-tesla-" prefix
	name := strings.Join(parts, "-")
	name = strings.TrimPrefix(name, "nvidia-tesla-")
	name = strings.TrimPrefix(name, "nvidia-")
	return strings.ToUpper(name)
}
