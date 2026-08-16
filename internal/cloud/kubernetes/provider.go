package kubernetes

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/acmestack/gpi/internal/cloud"
	"github.com/acmestack/gpi/internal/logging"
)

var logger = logging.WithName("kubernetes")

const (
	cloudName = "kubernetes"

	// labelPrefix is applied to all gpi-managed K8s resources.
	labelPrefix = "gpi.dev/"

	labelClusterName = labelPrefix + "cluster-name"
	labelRole        = labelPrefix + "role"
	labelManagedBy   = labelPrefix + "managed-by"

	defaultNamespace = "default"

	// maxClusterNameLen is the max length for K8s resource names (63 chars),
	// with 21 chars reserved for suffixes like "-head", "-worker0".
	maxClusterNameLen = 42
)

func init() {
	cloud.Register(Provider{})
	cloud.RegisterFactory(cloudName, func(creds *cloud.Credentials) (cloud.Provider, error) {
		return Provider{}, nil
	})
}

// Provider implements cloud.Provider for Kubernetes.
// Each kubeconfig context is treated as a region.
// Pods are treated as instances.
type Provider struct{}

func (Provider) Name() string { return cloudName }

// Regions returns all available kubeconfig contexts as regions.
func (Provider) Regions(ctx context.Context) ([]string, error) {
	return Contexts()
}

// RunInstances creates pods in the given context (region).
// The NamePrefix field on LaunchSpec is used as the cluster name.
func (Provider) RunInstances(ctx context.Context, spec *cloud.LaunchSpec) ([]*cloud.Instance, error) {
	context := spec.Region
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
	ns := defaultNamespace

	// Truncate cluster name to fit K8s 63-char limit with suffixes.
	clusterName := truncateName(spec.NamePrefix, maxClusterNameLen)

	var instances []*cloud.Instance
	for i := 0; i < spec.NumNodes; i++ {
		role := "worker"
		if i == 0 && spec.NumNodes > 1 {
			role = "head"
		}
		if spec.NumNodes == 1 {
			role = "head"
		}
		podName := clusterName
		if spec.NumNodes > 1 {
			if role == "head" {
				podName = podName + "-head"
			} else {
				podName = fmt.Sprintf("%s-worker%d", clusterName, i)
			}
		}

		// Delete stale pod if exists
		_ = cs.CoreV1().Pods(ns).Delete(ctx, podName, metav1.DeleteOptions{})

		pod := buildPod(podName, ns, spec, role)
		created, err := cs.CoreV1().Pods(ns).Create(ctx, pod, metav1.CreateOptions{})
		if err != nil {
			return nil, fmt.Errorf("create pod %s: %w", podName, err)
		}
		logger.Info("pod created", "name", podName, "namespace", ns, "context", context)

		inst := &cloud.Instance{
			ID:           string(created.UID),
			Name:         podName,
			InstanceType: spec.InstanceType,
			Region:       context,
			Status:       cloud.StatusPending,
			Tags:         spec.Tags,
		}
		instances = append(instances, inst)
	}
	return instances, nil
}

// buildPod constructs a Pod spec for a gpi cluster node.
func buildPod(name, namespace string, spec *cloud.LaunchSpec, role string) *corev1.Pod {
	labels := map[string]string{
		labelClusterName: spec.NamePrefix,
		labelRole:        role,
		labelManagedBy:   "gpi",
	}
	for k, v := range spec.Tags {
		labels[k] = v
	}

	// Parse resource requests from instance type (e.g. "4CPU--16GB--H100:1")
	cpus, memGiB, gpuType, gpuCount := parseInstanceType(spec.InstanceType)

	// Build environment variables
	envVars := []corev1.EnvVar{}
	if gpuCount == 0 {
		// Prevent GPU visibility when no GPU requested (isolation)
		envVars = append(envVars, corev1.EnvVar{
			Name:  "NVIDIA_VISIBLE_DEVICES",
			Value: "none",
		})
	}

	containers := []corev1.Container{{
		Name:  "ray-node",
		Image: spec.ImageID,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{},
			Limits:   corev1.ResourceList{},
		},
		Env: envVars,
	}}

	res := containers[0].Resources
	if cpus > 0 {
		q := resource.MustParse(strconv.Itoa(cpus))
		res.Requests[corev1.ResourceCPU] = q
		res.Limits[corev1.ResourceCPU] = q
	}
	if memGiB > 0 {
		q := resource.MustParse(fmt.Sprintf("%dGi", memGiB))
		res.Requests[corev1.ResourceMemory] = q
		res.Limits[corev1.ResourceMemory] = q
	}
	if gpuCount > 0 && gpuType != "" {
		// Determine resource key from detected GPU
		gpuResKey := resolveGPUResourceKey(gpuType)
		q := resource.MustParse(strconv.Itoa(gpuCount))
		res.Requests[corev1.ResourceName(gpuResKey)] = q
		res.Limits[corev1.ResourceName(gpuResKey)] = q
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Containers:    containers,
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	// Set node affinity if GPU is requested
	if gpuCount > 0 && gpuType != "" {
		result, err := DetectGPUForType(context.Background(), spec.Region, gpuType)
		if err == nil && result != nil && result.NodeAffinity != nil {
			pod.Spec.Affinity = &corev1.Affinity{
				NodeAffinity: result.NodeAffinity,
			}
		}
	}

	return pod
}

// parseInstanceType parses "4CPU--16GB--H100:1" into components.
func parseInstanceType(instanceType string) (cpus int, memGiB int, gpuType string, gpuCount int) {
	parts := strings.Split(instanceType, "--")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasSuffix(part, "CPU") {
			n, _ := strconv.Atoi(strings.TrimSuffix(part, "CPU"))
			cpus = n
		} else if strings.HasSuffix(part, "GB") {
			n, _ := strconv.Atoi(strings.TrimSuffix(part, "GB"))
			memGiB = n
		} else if idx := strings.Index(part, ":"); idx > 0 {
			gpuType = part[:idx]
			n, _ := strconv.Atoi(part[idx+1:])
			gpuCount = n
		} else if part != "" {
			// Bare GPU name like "H100" → count=1
			gpuType = part
			gpuCount = 1
		}
	}
	return
}

// truncateName truncates a name to maxLen characters, preserving DNS safety.
func truncateName(name string, maxLen int) string {
	if len(name) <= maxLen {
		return name
	}
	return name[:maxLen]
}

// resolveGPUResourceKey maps a GPU type string to a K8s resource key.
func resolveGPUResourceKey(gpuType string) string {
	upper := strings.ToUpper(gpuType)
	switch {
	case strings.Contains(upper, "TPU"):
		return "google.com/tpu"
	case strings.Contains(upper, "NEURON") || strings.Contains(upper, "TRAINIUM") || strings.Contains(upper, "INFERENTIA"):
		return "aws.amazon.com/neuron"
	default:
		return "nvidia.com/gpu"
	}
}

// ListInstances lists pods matching the name prefix in a context.
func (Provider) ListInstances(ctx context.Context, region, namePrefix string) ([]*cloud.Instance, error) {
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
	ns := defaultNamespace
	labelSelector := labelClusterName + "=" + namePrefix
	pods, err := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, err
	}
	var instances []*cloud.Instance
	for _, pod := range pods.Items {
		inst := podToInstance(&pod, context)
		instances = append(instances, inst)
	}
	return instances, nil
}

// DescribeInstances returns details for specific pod UIDs.
func (Provider) DescribeInstances(ctx context.Context, region string, ids []string) ([]*cloud.Instance, error) {
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
	ns := defaultNamespace
	var instances []*cloud.Instance
	for _, uid := range ids {
		pods, err := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
			LabelSelector: labelManagedBy + "=gpi",
		})
		if err != nil {
			return nil, err
		}
		for _, pod := range pods.Items {
			if string(pod.UID) == uid {
				instances = append(instances, podToInstance(&pod, context))
				break
			}
		}
	}
	return instances, nil
}

// StopInstances is not supported on Kubernetes (pods cannot be "stopped").
func (Provider) StopInstances(_ context.Context, _ string, _ []string) error {
	return fmt.Errorf("stop is not supported on Kubernetes")
}

// StartInstances is not supported on Kubernetes.
func (Provider) StartInstances(_ context.Context, _ string, _ []string) error {
	return fmt.Errorf("start is not supported on Kubernetes")
}

// TerminateInstances deletes pods by name.
func (Provider) TerminateInstances(ctx context.Context, region string, ids []string) error {
	context := region
	if context == "" {
		var err error
		context, err = CurrentContext()
		if err != nil {
			return err
		}
	}
	cs, err := clientFor(context)
	if err != nil {
		return err
	}
	ns := defaultNamespace
	for _, uid := range ids {
		// uid is actually the pod name for K8s backend
		gracePeriod := int64(0)
		err := cs.CoreV1().Pods(ns).Delete(ctx, uid, metav1.DeleteOptions{
			GracePeriodSeconds: &gracePeriod,
		})
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("delete pod %s: %w", uid, err)
		}
		logger.Info("pod terminated", "name", uid)
	}
	return nil
}

// GetPublicIP returns the PodIP of a pod, or the LoadBalancer ingress IP
// if a Service exists for it.
func (Provider) GetPublicIP(ctx context.Context, region, id string) (string, error) {
	context := region
	if context == "" {
		var err error
		context, err = CurrentContext()
		if err != nil {
			return "", err
		}
	}
	cs, err := clientFor(context)
	if err != nil {
		return "", err
	}
	ns := defaultNamespace

	// Try to get the pod by name (id = pod name for K8s backend)
	pod, err := cs.CoreV1().Pods(ns).Get(ctx, id, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	if pod.Status.PodIP != "" {
		return pod.Status.PodIP, nil
	}
	return pod.Status.HostIP, nil
}

// DescribeZones returns empty (K8s has no zone concept).
func (Provider) DescribeZones(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

// CreateKeyPair is a no-op for Kubernetes (uses ServiceAccount/RBAC).
func (Provider) CreateKeyPair(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

// DeleteKeyPair is a no-op for Kubernetes.
func (Provider) DeleteKeyPair(_ context.Context, _, _ string) error {
	return nil
}

// CreateSecurityGroup creates a NetworkPolicy (placeholder).
func (Provider) CreateSecurityGroup(_ context.Context, _, _, _, _ string) (string, error) {
	// TODO: implement NetworkPolicy creation
	return "", nil
}

// AuthorizeSecurityGroup is a no-op placeholder.
func (Provider) AuthorizeSecurityGroup(_ context.Context, _, _ string, _, _ int, _ string) error {
	return nil
}

// CreateVPC creates a Namespace (K8s "VPC" equivalent).
func (Provider) CreateVPC(ctx context.Context, region, cidr, name string) (string, error) {
	context := region
	if context == "" {
		var err error
		context, err = CurrentContext()
		if err != nil {
			return "", err
		}
	}
	cs, err := clientFor(context)
	if err != nil {
		return "", err
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				labelManagedBy: "gpi",
			},
		},
	}
	_, err = cs.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return "", fmt.Errorf("create namespace %s: %w", name, err)
	}
	return name, nil
}

// CreateVSwitch is a no-op for Kubernetes.
func (Provider) CreateVSwitch(_ context.Context, _, _, _, _, _ string) (string, error) {
	return "", nil
}

// ListVSwitches returns empty for Kubernetes.
func (Provider) ListVSwitches(_ context.Context, _, _ string) ([]cloud.VSwitch, error) {
	return nil, nil
}

// GetImage returns the container image to use.
func (Provider) GetImage(_ context.Context, _, platform string) (string, error) {
	// Default gpi base image
	return "ghcr.io/acmestack/gpi-base:latest", nil
}

// podToInstance converts a K8s Pod to a gpi cloud.Instance.
func podToInstance(pod *corev1.Pod, context string) *cloud.Instance {
	status := cloud.StatusUnknown
	switch pod.Status.Phase {
	case corev1.PodPending:
		status = cloud.StatusPending
	case corev1.PodRunning:
		status = cloud.StatusRunning
	case corev1.PodSucceeded, corev1.PodFailed:
		status = cloud.StatusTerminated
	}
	inst := &cloud.Instance{
		ID:           string(pod.UID),
		Name:         pod.Name,
		InstanceType: pod.Labels[labelClusterName],
		Region:       context,
		Status:       status,
	}
	if pod.Status.PodIP != "" {
		inst.PrivateIP = pod.Status.PodIP
		inst.PublicIP = pod.Status.PodIP
	}
	return inst
}
