package kubernetes

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GPULabelFormatter defines a known GPU labeling scheme on K8s nodes.
type GPULabelFormatter struct {
	Name      string
	LabelKey  string
	ValueFunc func(raw string) string // transforms label value to accelerator name
}

// Known formatters, ordered by priority (most common first).
var gpuFormatters = []GPULabelFormatter{
	{
		Name:     "GKE",
		LabelKey: "cloud.google.com/gke-accelerator",
		ValueFunc: func(v string) string {
			// "nvidia-tesla-v100" → "V100", "nvidia-l4" → "L4"
			v = strings.TrimPrefix(v, "nvidia-")
			v = strings.TrimPrefix(v, "tesla-")
			return strings.ToUpper(v)
		},
	},
	{
		Name:     "GFD",
		LabelKey: "nvidia.com/gpu.product",
		ValueFunc: func(v string) string {
			// "NVIDIA-H100-80GB-HBM3" → "H100"
			v = strings.TrimPrefix(v, "NVIDIA-")
			parts := strings.Split(v, "-")
			if len(parts) > 0 {
				return strings.ToUpper(parts[0])
			}
			return strings.ToUpper(v)
		},
	},
	{
		Name:      "Karpenter",
		LabelKey:  "karpenter.k8s.aws/instance-gpu-name",
		ValueFunc: func(v string) string { return strings.ToUpper(v) },
	},
	{
		Name:      "CoreWeave",
		LabelKey:  "gpu.nvidia.com/class",
		ValueFunc: func(v string) string { return strings.ToUpper(v) },
	},
	{
		Name:      "GPI",
		LabelKey:  "gpi.dev/accelerator",
		ValueFunc: func(v string) string { return strings.ToUpper(v) },
	},
}

// GPUResourceKeys lists K8s resource keys to check for GPU availability.
var GPUResourceKeys = []string{
	"nvidia.com/gpu",
	"amd.com/gpu",
	"google.com/tpu",
	"aws.amazon.com/neuron",
}

// DetectGPU scans node labels and returns the detected GPU type and count,
// or ("", 0) if no GPU nodes found. Also returns the label key and affinity
// terms for injection into pod specs.
type GPUDetectResult struct {
	AcceleratorType  string
	AcceleratorCount int
	LabelKey         string
	LabelValue       string
	ResourceKey      string
	NodeAffinity     *corev1.NodeAffinity
}

// DetectGPU discovers GPU capabilities across all nodes in a context.
func DetectGPU(ctx context.Context, context string, requestedGPU string) (*GPUDetectResult, error) {
	cs, err := clientFor(context)
	if err != nil {
		return nil, err
	}
	nodes, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	if len(nodes.Items) == 0 {
		return nil, nil
	}

	// Scan nodes for GPU labels
	for _, node := range nodes.Items {
		labels := node.Labels
		for _, f := range gpuFormatters {
			raw, ok := labels[f.LabelKey]
			if !ok {
				continue
			}
			accelType := f.ValueFunc(raw)
			if accelType == "" {
				continue
			}
			// Find GPU resource on this node
			for _, resKey := range GPUResourceKeys {
				if qty, ok := node.Status.Allocatable[corev1.ResourceName(resKey)]; ok {
					count := int(qty.Value())
					if count > 0 {
						return &GPUDetectResult{
							AcceleratorType:  accelType,
							AcceleratorCount: count,
							LabelKey:         f.LabelKey,
							LabelValue:       raw,
							ResourceKey:      resKey,
							NodeAffinity:     buildNodeAffinity(f.LabelKey, raw),
						}, nil
					}
				}
			}
		}
	}
	return nil, nil
}

// DetectGPUForType detects GPU nodes matching a specific accelerator type.
func DetectGPUForType(ctx context.Context, context, accelType string) (*GPUDetectResult, error) {
	cs, err := clientFor(context)
	if err != nil {
		return nil, err
	}
	nodes, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	upperType := strings.ToUpper(accelType)

	for _, node := range nodes.Items {
		labels := node.Labels
		for _, f := range gpuFormatters {
			raw, ok := labels[f.LabelKey]
			if !ok {
				continue
			}
			detected := f.ValueFunc(raw)
			if strings.ToUpper(detected) != upperType {
				continue
			}
			for _, resKey := range GPUResourceKeys {
				if qty, ok := node.Status.Allocatable[corev1.ResourceName(resKey)]; ok {
					count := int(qty.Value())
					if count > 0 {
						return &GPUDetectResult{
							AcceleratorType:  detected,
							AcceleratorCount: count,
							LabelKey:         f.LabelKey,
							LabelValue:       raw,
							ResourceKey:      resKey,
							NodeAffinity:     buildNodeAffinity(f.LabelKey, raw),
						}, nil
					}
				}
			}
		}
	}
	return nil, nil
}

// buildNodeAffinity creates a NodeAffinity with RequiredDuringScheduling.
func buildNodeAffinity(labelKey, labelValue string) *corev1.NodeAffinity {
	return &corev1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      labelKey,
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{labelValue},
						},
					},
				},
			},
		},
	}
}
