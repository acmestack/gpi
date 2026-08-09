package aliyun

import (
	"context"
	"strings"
	"time"

	"github.com/acmestack/gpi/internal/cloud/catalog"
)

// instanceTypeItem is one entry of DescribeInstanceTypes.
type instanceTypeItem struct {
	InstanceTypeId string  `json:"InstanceTypeId"`
	CpuCoreCount   int     `json:"CpuCoreCount"`
	MemorySize     float64 `json:"MemorySize"`
	GPUAmount      int     `json:"GPUAmount"`
	GPUSpec        string  `json:"GPUSpec"`
}

type instanceTypesResp struct {
	InstanceTypes struct {
		InstanceType []instanceTypeItem `json:"InstanceType"`
	} `json:"InstanceTypes"`
	NextToken string `json:"NextToken"`
}

// DescribeInstanceTypes returns the specs of instance types available in
// region, following NextToken pagination.
func (c *Client) DescribeInstanceTypes(ctx context.Context, region string) ([]instanceTypeItem, error) {
	var out []instanceTypeItem
	params := map[string]string{
		"RegionId":   region,
		"MaxResults": "100",
	}
	for {
		var page instanceTypesResp
		if err := c.call(ctx, "DescribeInstanceTypes", params, &page); err != nil {
			return nil, err
		}
		out = append(out, page.InstanceTypes.InstanceType...)
		if page.NextToken == "" {
			break
		}
		params["NextToken"] = page.NextToken
	}
	return out, nil
}

// toSpec converts a raw instance-type item into a catalog.Instance. Names
// like "NVIDIA T4" / "T4" are normalized to the bare accelerator name.
func toSpec(c string, region string, it instanceTypeItem) *catalog.Instance {
	accel := map[string]int{}
	if it.GPUAmount > 0 {
		name := strings.TrimSpace(it.GPUSpec)
		if i := strings.LastIndex(name, " "); i >= 0 {
			name = name[i+1:]
		}
		if name == "" {
			name = "gpu"
		}
		accel[name] = it.GPUAmount
	}
	return &catalog.Instance{
		Cloud:        c,
		Region:       region,
		InstanceType: it.InstanceTypeId,
		VCPUs:        it.CpuCoreCount,
		MemoryGiB:    it.MemorySize,
		Accelerators: accel,
		MaxDiskGiB:   500,
	}
}

// specsTTL and priceTTL are per-cloud TTLs: specs change rarely (refresh
// daily), prices change often (refresh every 10 minutes).
const (
	specsTTL = 24 * time.Hour
	priceTTL = 10 * time.Minute
)

// metadataClient returns a client for metadata (specs/prices) fetches, bound
// to the provider's credentials when set, else loaded from env/disk.
func (p Provider) metadataClient(region string) (*Client, error) {
	if p.creds != nil {
		return NewClientWithCreds(region, *p.creds)
	}
	return NewClient(region)
}

// The Source methods below live on Provider so a single struct implements
// both cloud.Provider and catalog.Source; cloud.Register auto-registers the
// metadata source when the provider satisfies it.

func (p Provider) Cloud() string { return "aliyun" }

func (p Provider) SpecsTTL() time.Duration { return specsTTL }

func (p Provider) PriceTTL() time.Duration { return priceTTL }

func (p Provider) FetchSpecs(ctx context.Context, region string) ([]*catalog.Instance, error) {
	c, err := p.metadataClient(region)
	if err != nil {
		return nil, err
	}
	raw, err := c.DescribeInstanceTypes(ctx, region)
	if err != nil {
		return nil, err
	}
	out := make([]*catalog.Instance, 0, len(raw))
	for _, it := range raw {
		out = append(out, toSpec("aliyun", region, it))
	}
	return out, nil
}

func (p Provider) FetchPrices(ctx context.Context, region string, instanceTypes []string) (map[string]catalog.Price, error) {
	c, err := p.metadataClient(region)
	if err != nil {
		return nil, err
	}
	if len(instanceTypes) == 0 {
		return map[string]catalog.Price{}, nil
	}
	onDemand, _ := c.DescribeOnDemandPrice(ctx, region, instanceTypes)
	spot, _ := c.DescribeSpotPriceHistory(ctx, region, instanceTypes)
	out := map[string]catalog.Price{}
	for _, it := range instanceTypes {
		p := catalog.Price{OnDemand: onDemand[it], Spot: spot[it]}
		if p.OnDemand > 0 || p.Spot > 0 {
			out[it] = p
		}
	}
	// A partially-failed refresh still returns what we got; the cache merges
	// and the caller falls back to stale prices for anything missing.
	return out, nil
}
