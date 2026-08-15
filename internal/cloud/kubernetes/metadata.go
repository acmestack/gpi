package kubernetes

import (
	"context"
	"fmt"
	"time"

	"github.com/acmestack/gpi/internal/cloud/catalog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Ensure Provider implements catalog.Source at compile time.
var _ catalog.Source = Provider{}

// Cloud returns the cloud name.
func (Provider) Cloud() string { return cloudName }

// SpecsTTL — node specs change rarely.
func (Provider) SpecsTTL() time.Duration { return 24 * time.Hour }

// PriceTTL — K8s has no billing, but keep a short TTL for consistency.
func (Provider) PriceTTL() time.Duration { return 10 * time.Minute }

// FetchSpecs queries node allocatable resources and returns virtual instance
// types in the standard gpi format: "{cpus}CPU--{memGB}GB" or
// "{cpus}CPU--{memGB}GB--{gpu}:{count}".
func (Provider) FetchSpecs(ctx context.Context, region string) ([]*catalog.Instance, error) {
	context := region
	if context == "" {
		var err error
		context, err = CurrentContext()
		if err != nil {
			return nil, err
		}
	}
	cs, err := clientFor(context)
	if err != nil {
		return nil, err
	}
	nodes, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	// Deduplicate by resource shape (nodes with same allocatable → same spec)
	seen := map[string]*catalog.Instance{}

	for _, node := range nodes.Items {
		alloc := node.Status.Allocatable
		cpus := int(alloc.Cpu().Value())
		memGiB := int(alloc.Memory().Value() / (1024 * 1024 * 1024))

		// Detect GPUs
		gpuType := ""
		gpuCount := 0
		for _, f := range gpuFormatters {
			if raw, ok := node.Labels[f.LabelKey]; ok {
				gpuType = f.ValueFunc(raw)
				for _, resKey := range GPUResourceKeys {
					if qty, ok := alloc[corev1.ResourceName(resKey)]; ok {
						gpuCount = int(qty.Value())
						if gpuCount > 0 {
							break
						}
					}
				}
				if gpuCount > 0 {
					break
				}
			}
		}

		// Build instance type name
		instType := instanceTypeName(cpus, memGiB, gpuType, gpuCount)
		if _, ok := seen[instType]; ok {
			continue
		}

		accel := map[string]int{}
		if gpuCount > 0 && gpuType != "" {
			accel[gpuType] = gpuCount
		}

		seen[instType] = &catalog.Instance{
			Cloud:        cloudName,
			Region:       context,
			InstanceType: instType,
			VCPUs:        cpus,
			MemoryGiB:    float64(memGiB),
			Accelerators: accel,
		}
	}

	specs := make([]*catalog.Instance, 0, len(seen))
	for _, s := range seen {
		specs = append(specs, s)
	}
	return specs, nil
}

// instanceTypeName builds a canonical instance type string.
func instanceTypeName(cpus, memGiB int, gpuType string, gpuCount int) string {
	base := fmt.Sprintf("%dCPU--%dGB", cpus, memGiB)
	if gpuCount > 0 && gpuType != "" {
		base += fmt.Sprintf("--%s:%d", gpuType, gpuCount)
	}
	return base
}

// FetchPrices returns $0 for all instance types (K8s is self-hosted).
func (Provider) FetchPrices(_ context.Context, _ string, instanceTypes []string) (map[string]catalog.Price, error) {
	prices := make(map[string]catalog.Price, len(instanceTypes))
	for _, it := range instanceTypes {
		prices[it] = catalog.Price{OnDemand: 0, Spot: 0}
	}
	return prices, nil
}
