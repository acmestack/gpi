package kubernetes

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

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

	// defaultBaseImage is the prebuilt gpi base image (gpilet + Ray); see
	// Dockerfile.gpi-base. Overridable via the kubernetes config section.
	defaultBaseImage = "ghcr.io/acmestack/gpi-base:latest"

	// maxClusterNameLen is the max length for K8s resource names (63 chars),
	// with 21 chars reserved for suffixes like "-head", "-worker0".
	maxClusterNameLen = 42

	// Env vars injected into every gpi node pod to bootstrap gpilet + Ray.
	envRole     = "GPI_ROLE"
	envPodIP    = "GPI_POD_IP"
	envHeadAddr = "GPI_HEAD_ADDR"

	// Ray ports (mirror SkyPilot defaults for user Ray clusters).
	rayHeadPort       = 6379
	rayDashboardPort  = 8265
	gpiletBin         = "/usr/local/bin/gpilet"
	gpiletDir         = "/var/lib/gpilet"
	gpiletIntervalSec = 10

	// Pod readiness wait: per-attempt timeout and retry count used by
	// RunInstances when waiting for a node pod to reach Running.
	podWaitTimeout = 120 * time.Second
	podWaitRetries = 3
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
	cfg := LoadConfig()
	context := spec.Region
	if context == "" {
		if cfg != nil {
			context = cfg.Context
		}
		if context == "" {
			var err error
			context, err = CurrentContext()
			if err != nil {
				return nil, err
			}
		}
	}
	cs, err := clientFor(context)
	if err != nil {
		return nil, err
	}
	ns := defaultNamespace
	if cfg != nil {
		ns = cfg.EffectiveNamespace()
	}

	// Truncate cluster name to fit K8s 63-char limit with suffixes.
	clusterName := truncateName(spec.NamePrefix, maxClusterNameLen)

	// Create pods sequentially so the head's pod IP is known before workers
	// start, letting workers join the Ray cluster (SkyPilot-style bootstrap).
	var headAddr string
	var instances []*cloud.Instance
	var created []string

	// Pod readiness wait is configurable (pod_wait_timeout / pod_wait_retries)
	// so slow image pulls in CI don't fail a launch prematurely.
	timeout := podWaitTimeout
	retries := podWaitRetries
	if cfg != nil {
		timeout = cfg.EffectivePodWaitTimeout()
		retries = cfg.EffectivePodWaitRetries()
	}
	for i := 0; i < spec.NumNodes; i++ {
		role := "worker"
		if i == 0 {
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

		pod := buildPod(podParams{
			Name:      podName,
			Namespace: ns,
			Spec:      spec,
			Role:      role,
			HeadAddr:  headAddr,
			Cfg:       cfg,
		})
		_, err = cs.CoreV1().Pods(ns).Create(ctx, pod, metav1.CreateOptions{})
		if err != nil {
			return nil, fmt.Errorf("create pod %s: %w", podName, err)
		}
		logger.Info("pod created", "name", podName, "namespace", ns, "context", context)
		created = append(created, podName)

		// Wait for the pod to reach Running. Image pull + container start can
		// take a while on first CI run, so retry (pod_wait_retries attempts of
		// pod_wait_timeout each); if it still is not Running, fail for real and
		// clean up the pods created so far.
		if err := waitPodReady(ctx, cs, ns, podName, timeout, retries); err != nil {
			cleanupPods(ctx, cs, ns, created)
			return nil, err
		}

		// ID is the pod name so TerminateInstances can delete by name (the
		// pod UID is not accepted by the delete API).
		inst := &cloud.Instance{
			ID:           podName,
			Name:         podName,
			InstanceType: spec.InstanceType,
			Region:       context,
			Status:       cloud.StatusRunning,
			Tags:         spec.Tags,
		}
		instances = append(instances, inst)

		// Capture the head pod's IP so workers can join its Ray cluster.
		if role == "head" && spec.NumNodes > 1 {
			headAddr = waitPodIP(ctx, cs, ns, podName, timeout)
			logger.Info("head pod ip", "name", podName, "ip", headAddr)
			if headAddr == "" {
				cleanupPods(ctx, cs, ns, created)
				return nil, fmt.Errorf("head pod %s: no pod IP within %v", podName, timeout)
			}
		}
	}
	return instances, nil
}

// waitPodReady waits for a pod to reach Running, retrying up to retries
// attempts of timeout each. It returns a descriptive error when the pod never
// becomes Running.
func waitPodReady(ctx context.Context, cs *kubernetes.Clientset, ns, name string, timeout time.Duration, retries int) error {
	for attempt := 1; attempt <= retries; attempt++ {
		if waitPodRunning(ctx, cs, ns, name, timeout) {
			return nil
		}
		logger.Info("pod not running, retrying",
			"name", name, "attempt", attempt, "of", retries, "timeout", timeout)
	}
	return fmt.Errorf("pod %s did not reach Running within %d attempts of %v", name, retries, timeout)
}

// cleanupPods deletes the given pods (best-effort), used when RunInstances
// fails partway through a multi-node launch.
func cleanupPods(ctx context.Context, cs *kubernetes.Clientset, ns string, names []string) {
	grace := int64(0)
	for _, n := range names {
		if err := cs.CoreV1().Pods(ns).Delete(ctx, n, metav1.DeleteOptions{GracePeriodSeconds: &grace}); err != nil {
			if !errors.IsNotFound(err) {
				logger.Info("cleanup pod failed", "name", n, "err", err)
			}
			continue
		}
		logger.Info("pod cleaned up", "name", n)
	}
}

// waitPodRunning polls a pod until it reaches Running or a timeout elapses.
func waitPodRunning(ctx context.Context, cs *kubernetes.Clientset, ns, name string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pod, err := cs.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
		if err == nil && pod.Status.Phase == corev1.PodRunning {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// waitPodIP polls a pod until it has a PodIP or a timeout elapses.
func waitPodIP(ctx context.Context, cs *kubernetes.Clientset, ns, name string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pod, err := cs.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
		if err == nil && pod.Status.PodIP != "" {
			return pod.Status.PodIP
		}
		time.Sleep(500 * time.Millisecond)
	}
	return ""
}

// podParams carries everything needed to build a single gpi node pod.
// Grouped into one struct so buildPod keeps a minimal, stable signature and
// new knobs (affinity, tolerations, ...) only add a field here.
type podParams struct {
	// Name and Namespace of the pod.
	Name      string
	Namespace string
	// Spec is the launch spec (instance type, image, tags, region).
	Spec *cloud.LaunchSpec
	// Role is "head" or "worker".
	Role string
	// HeadAddr is the head pod's IP; set only for workers (empty for head).
	HeadAddr string
	// Cfg is the kubernetes config section; may be nil (defaults used).
	Cfg *Config
}

// buildPod constructs a Pod spec for a gpi cluster node.
func buildPod(p podParams) *corev1.Pod {
	name, namespace, spec, role, headAddr, cfg := p.Name, p.Namespace, p.Spec, p.Role, p.HeadAddr, p.Cfg
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
	envVars := []corev1.EnvVar{
		{Name: envRole, Value: role},
	}
	if gpuCount == 0 {
		// Prevent GPU visibility when no GPU requested (isolation)
		envVars = append(envVars, corev1.EnvVar{
			Name:  "NVIDIA_VISIBLE_DEVICES",
			Value: "none",
		})
	}
	if role == "head" {
		// Inject own pod IP so `ray start --head` binds the right address.
		envVars = append(envVars, corev1.EnvVar{
			Name: envPodIP,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
			},
		})
	} else if headAddr != "" {
		// Workers join the head's Ray cluster.
		envVars = append(envVars, corev1.EnvVar{Name: envHeadAddr, Value: headAddr})
	}

	containers := []corev1.Container{{
		Name:            "ray-node",
		Image:           spec.ImageID,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{},
			Limits:   corev1.ResourceList{},
		},
		Env:     envVars,
		Command: podStartupCommand(cfg, role),
		// Ray's object store needs a larger /dev/shm than Kubernetes'
		// default (64Mi); mount an in-memory emptyDir (Ray standard setup).
		VolumeMounts: []corev1.VolumeMount{
			{Name: "dshm", MountPath: "/dev/shm"},
		},
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
			Volumes: []corev1.Volume{
				{Name: "dshm", VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory},
				}},
			},
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

// podStartupCommand returns the container command that bootstraps gpilet and
// Ray inside a gpi node pod (SkyPilot-style: head starts Ray, workers join it).
func podStartupCommand(cfg *Config, role string) []string {
	gd := gpiletDir
	if cfg != nil {
		gd = cfg.EffectiveGpiletDir()
	}
	interval := gpiletIntervalSec
	if cfg != nil {
		interval = cfg.EffectiveGpiletInterval()
	}
	headPort := rayHeadPort
	if cfg != nil {
		headPort = cfg.EffectiveRayHeadPort()
	}
	dashPort := rayDashboardPort
	if cfg != nil {
		dashPort = cfg.EffectiveRayDashboardPort()
	}

	startGpilet := gpiletBin + " serve --dir " + gd + " --interval " + strconv.Itoa(interval)

	rayStart := ""
	if role == "head" {
		rayStart = "ray start --head --port=" + strconv.Itoa(headPort) +
			" --dashboard-port=" + strconv.Itoa(dashPort) +
			" --disable-usage-stats --node-ip-address=$GPI_POD_IP"
	} else {
		rayStart = "until ray start --address=$GPI_HEAD_ADDR:" + strconv.Itoa(headPort) +
			" --disable-usage-stats; do sleep 2; done"
	}

	script := "set -e\n" +
		"nohup " + startGpilet + " > /var/log/gpilet.log 2>&1 &\n" +
		rayStart + "\n" +
		"tail -f /dev/null\n"

	return []string{"/bin/sh", "-c", script}
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

// DescribeInstances returns details for the given pods, matched by pod name
// (the Instance ID). UID matches are also accepted for backward compatibility.
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
	for _, id := range ids {
		pods, err := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
			LabelSelector: labelManagedBy + "=gpi",
		})
		if err != nil {
			return nil, err
		}
		for _, pod := range pods.Items {
			if pod.Name == id || string(pod.UID) == id {
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

// GetImage returns the container image to use. The default gpi base image
// (see Dockerfile.gpi-base) carries gpilet and Ray; it can be overridden via
// the "kubernetes" section of the gpi config (image: ...).
func (Provider) GetImage(_ context.Context, _, _ string) (string, error) {
	if c := LoadConfig(); c != nil && c.Image != "" {
		return c.Image, nil
	}
	return defaultBaseImage, nil
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
		ID:           pod.Name,
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
