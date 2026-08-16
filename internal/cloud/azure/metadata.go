package azure

import (
	"context"
	"time"

	"github.com/acmestack/gpi/internal/cloud/catalog"
)

// Ensure Provider implements catalog.Source at compile time.
var _ catalog.Source = Provider{}

// Cloud returns the cloud name.
func (Provider) Cloud() string { return CloudName }

// SpecsTTL — VM size specs change rarely.
func (Provider) SpecsTTL() time.Duration { return 24 * time.Hour }

// PriceTTL — Azure prices change occasionally.
func (Provider) PriceTTL() time.Duration { return 10 * time.Minute }

// FetchSpecs queries available VM sizes in a region.
func (p Provider) FetchSpecs(ctx context.Context, region string) ([]*catalog.Instance, error) {
	c, err := p.client(region)
	if err != nil {
		return nil, err
	}

	type vmSizeItem struct {
		Name             string `json:"name"`
		NumberOfCores    int    `json:"numberOfCores"`
		MemoryInMB       int    `json:"memoryInMB"`
		MaxDataDiskCount int    `json:"maxDataDiskCount"`
	}
	type listResponse struct {
		Value []vmSizeItem `json:"value"`
	}

	path := "/providers/Microsoft.Compute/locations/" + region + "/vmSizes"
	var resp listResponse
	if err := c.call(ctx, path, &resp); err != nil {
		return nil, err
	}

	var specs []*catalog.Instance
	for _, item := range resp.Value {
		memGiB := float64(item.MemoryInMB) / 1024.0
		accel := map[string]int{}
		specs = append(specs, &catalog.Instance{
			Cloud:        CloudName,
			Region:       region,
			InstanceType: item.Name,
			VCPUs:        item.NumberOfCores,
			MemoryGiB:    memGiB,
			Accelerators: accel,
		})
	}
	return specs, nil
}

// FetchPrices returns prices for VM sizes.
// Azure pricing requires the Retail Prices API (complex); return zeros for now.
func (p Provider) FetchPrices(ctx context.Context, region string, instanceTypes []string) (map[string]catalog.Price, error) {
	prices := make(map[string]catalog.Price, len(instanceTypes))
	for _, it := range instanceTypes {
		prices[it] = catalog.Price{OnDemand: 0, Spot: 0}
	}
	return prices, nil
}
