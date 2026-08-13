package aws

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/acmestack/gpi/internal/cloud/catalog"
)

// instanceTypeItem is one entry of DescribeInstanceTypes.
type instanceTypeItem struct {
	InstanceType string `xml:"instanceType"`
	VcpuInfo     struct {
		DefaultVCpus int `xml:"defaultVCpus"`
	} `xml:"vcpuInfo"`
	MemoryInfo struct {
		SizeInMiB int `xml:"sizeInMiB"`
	} `xml:"memoryInfo"`
	GpuInfo struct {
		Gpus struct {
			Item []struct {
				Name  string `xml:"name"`
				Count int    `xml:"count"`
			} `xml:"item"`
		} `xml:"gpus"`
	} `xml:"gpuInfo"`
}

type instanceTypesResp struct {
	InstanceTypeInfoSet struct {
		Item []instanceTypeItem `xml:"item"`
	} `xml:"instanceTypeInfoSet"`
	NextToken string `xml:"nextToken"`
}

// DescribeInstanceTypes returns the specs of all known instance types
// (global, not region-scoped), following NextToken pagination.
func (c *Client) DescribeInstanceTypes(ctx context.Context) ([]instanceTypeItem, error) {
	var out []instanceTypeItem
	params := map[string]string{"MaxResults": "100"}
	for {
		var page instanceTypesResp
		if err := c.call(ctx, "DescribeInstanceTypes", params, &page); err != nil {
			return nil, err
		}
		out = append(out, page.InstanceTypeInfoSet.Item...)
		if page.NextToken == "" {
			break
		}
		params["NextToken"] = page.NextToken
	}
	return out, nil
}

// instanceTypeOfferingsResp is the response of DescribeInstanceTypeOfferings
// filtered to a region, listing which instance types are actually offered.
type instanceTypeOfferingsResp struct {
	InstanceTypeOfferings struct {
		Item []struct {
			InstanceType string `xml:"instanceType"`
		} `xml:"item"`
	} `xml:"instanceTypeOfferingSet"`
	NextToken string `xml:"nextToken"`
}

// RegionInstanceTypes returns the instance types offered in region.
func (c *Client) RegionInstanceTypes(ctx context.Context, region string) (map[string]bool, error) {
	offered := map[string]bool{}
	params := map[string]string{
		"LocationType":     "region",
		"MaxResults":       "100",
		"Filter.1.Name":    "location",
		"Filter.1.Value.1": region,
	}
	for {
		var page instanceTypeOfferingsResp
		if err := c.call(ctx, "DescribeInstanceTypeOfferings", params, &page); err != nil {
			return nil, err
		}
		for _, it := range page.InstanceTypeOfferings.Item {
			offered[it.InstanceType] = true
		}
		if page.NextToken == "" {
			break
		}
		params["NextToken"] = page.NextToken
	}
	return offered, nil
}

// toSpec converts a raw AWS instance-type item into a catalog.Instance. GPU
// names come back like "T4"/"A10G"/"V100SXM2-32GB"; normalize to the bare
// accelerator family where possible.
func toSpec(c string, region string, it instanceTypeItem) *catalog.Instance {
	accel := map[string]int{}
	if len(it.GpuInfo.Gpus.Item) > 0 {
		var count int
		var name string
		for _, g := range it.GpuInfo.Gpus.Item {
			count += g.Count
			name = g.Name
		}
		name = normalizeAccel(name)
		if name == "" {
			name = "gpu"
		}
		accel[name] = count
	}
	return &catalog.Instance{
		Cloud:        c,
		Region:       region,
		InstanceType: it.InstanceType,
		VCPUs:        it.VcpuInfo.DefaultVCpus,
		MemoryGiB:    float64(it.MemoryInfo.SizeInMiB) / 1024,
		Accelerators: accel,
		MaxDiskGiB:   500,
	}
}

// normalizeAccel maps AWS GPU spec names to bare accelerator names.
func normalizeAccel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}
	switch {
	case strings.HasPrefix(name, "T4"):
		return "T4"
	case strings.HasPrefix(name, "A10"):
		return "A10"
	case strings.HasPrefix(name, "A100"):
		return "A100"
	case strings.HasPrefix(name, "A800"):
		return "A800"
	case strings.HasPrefix(name, "V100"):
		return "V100"
	case strings.HasPrefix(name, "H100"):
		return "H100"
	case strings.HasPrefix(name, "K80"):
		return "K80"
	case strings.HasPrefix(name, "M60"):
		return "M60"
	case strings.HasPrefix(name, "P4"):
		return "P4"
	case strings.HasPrefix(name, "P40"):
		return "P40"
	case strings.HasPrefix(name, "G4AD"):
		return "A10"
	case strings.HasPrefix(name, "G5G"):
		return "A10"
	}
	// Fall back to the token before the first '-' or ':'.
	if i := strings.IndexAny(name, "-:"); i > 0 {
		return name[:i]
	}
	return name
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
		cred := *p.creds
		if region != "" {
			cred.Region = region
		}
		return NewClientWithCreds(region, cred)
	}
	return NewClient(region)
}

// The Source methods below live on Provider so a single struct implements
// both cloud.Provider and catalog.Source; cloud.Register auto-registers the
// metadata source when the provider satisfies it.

func (p Provider) Cloud() string { return CloudName }

func (p Provider) SpecsTTL() time.Duration { return specsTTL }

func (p Provider) PriceTTL() time.Duration { return priceTTL }

func (p Provider) FetchSpecs(ctx context.Context, region string) ([]*catalog.Instance, error) {
	c, err := p.metadataClient(region)
	if err != nil {
		return nil, err
	}
	global, err := c.DescribeInstanceTypes(ctx)
	if err != nil {
		return nil, err
	}
	offered, err := c.RegionInstanceTypes(ctx, region)
	if err != nil {
		return nil, err
	}
	out := make([]*catalog.Instance, 0, len(global))
	for _, it := range global {
		if !offered[it.InstanceType] {
			continue
		}
		out = append(out, toSpec(CloudName, region, it))
	}
	return out, nil
}

func (p Provider) FetchPrices(ctx context.Context, region string, instanceTypes []string) (map[string]catalog.Price, error) {
	cred, err := p.metadataCreds()
	if err != nil {
		return nil, err
	}
	if len(instanceTypes) == 0 {
		return map[string]catalog.Price{}, nil
	}
	ec2, err := NewClientWithCreds(region, cred)
	if err != nil {
		return nil, err
	}
	spot, _ := ec2.DescribeSpotPriceHistory(ctx, region, instanceTypes)
	pc := newPricingClient(cred, region)

	var (
		mu  sync.Mutex
		out = map[string]catalog.Price{}
	)
	sem := make(chan struct{}, priceWorkers)
	var wg sync.WaitGroup
	for _, it := range instanceTypes {
		it := it
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			p := catalog.Price{Spot: spot[it]}
			if od, err := pc.getOnDemandPrice(ctx, region, it); err == nil {
				p.OnDemand = od
			}
			if p.OnDemand > 0 || p.Spot > 0 {
				mu.Lock()
				out[it] = p
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out, nil
}

const priceWorkers = 8

// metadataCreds returns the credentials to use for price fetches: the
// provider-bound creds when set, else the env/disk credentials.
func (p Provider) metadataCreds() (Credentials, error) {
	if p.creds != nil {
		return *p.creds, nil
	}
	return LoadCredentials()
}
