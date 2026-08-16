package kubernetes

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acmestack/gpi/internal/cloud"
	"github.com/acmestack/gpi/internal/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// envValue returns the value (or "" when not set) of an env var on a container.
func envValue(pod *corev1.Pod, name string) string {
	for _, env := range pod.Spec.Containers[0].Env {
		if env.Name == name {
			return env.Value
		}
	}
	return ""
}

func TestParseInstanceType(t *testing.T) {
	tests := []struct {
		input    string
		cpus     int
		memGiB   int
		gpuType  string
		gpuCount int
	}{
		{"4CPU--16GB", 4, 16, "", 0},
		{"8CPU--32GB", 8, 32, "", 0},
		{"4CPU--16GB--H100:1", 4, 16, "H100", 1},
		{"8CPU--64GB--A100:4", 8, 64, "A100", 4},
		{"4CPU--16GB--V100", 4, 16, "V100", 1},
		{"2CPU--8GB--T4:2", 2, 8, "T4", 2},
		{"16CPU--128GB--H100:8", 16, 128, "H100", 8},
	}
	for _, tt := range tests {
		cpus, memGiB, gpuType, gpuCount := parseInstanceType(tt.input)
		if cpus != tt.cpus {
			t.Errorf("parseInstanceType(%q) cpus = %d, want %d", tt.input, cpus, tt.cpus)
		}
		if memGiB != tt.memGiB {
			t.Errorf("parseInstanceType(%q) memGiB = %d, want %d", tt.input, memGiB, tt.memGiB)
		}
		if gpuType != tt.gpuType {
			t.Errorf("parseInstanceType(%q) gpuType = %q, want %q", tt.input, gpuType, tt.gpuType)
		}
		if gpuCount != tt.gpuCount {
			t.Errorf("parseInstanceType(%q) gpuCount = %d, want %d", tt.input, gpuCount, tt.gpuCount)
		}
	}
}

func TestResolveGPUResourceKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"H100", "nvidia.com/gpu"},
		{"A100", "nvidia.com/gpu"},
		{"V100", "nvidia.com/gpu"},
		{"T4", "nvidia.com/gpu"},
		{"TPU-V4", "google.com/tpu"},
		{"tpu-v5e", "google.com/tpu"},
		{"Trainium", "aws.amazon.com/neuron"},
		{"Inferentia", "aws.amazon.com/neuron"},
		{"NEURON", "aws.amazon.com/neuron"},
	}
	for _, tt := range tests {
		got := resolveGPUResourceKey(tt.input)
		if got != tt.want {
			t.Errorf("resolveGPUResourceKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestInstanceTypeName(t *testing.T) {
	tests := []struct {
		cpus     int
		memGiB   int
		gpuType  string
		gpuCount int
		want     string
	}{
		{4, 16, "", 0, "4CPU--16GB"},
		{8, 64, "H100", 1, "8CPU--64GB--H100:1"},
		{16, 128, "A100", 4, "16CPU--128GB--A100:4"},
	}
	for _, tt := range tests {
		got := instanceTypeName(tt.cpus, tt.memGiB, tt.gpuType, tt.gpuCount)
		if got != tt.want {
			t.Errorf("instanceTypeName(%d, %d, %q, %d) = %q, want %q",
				tt.cpus, tt.memGiB, tt.gpuType, tt.gpuCount, got, tt.want)
		}
	}
}

func TestPodToInstance(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster-head",
			UID:  "abc-123",
			Labels: map[string]string{
				labelClusterName: "test-cluster",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "10.0.0.5",
		},
	}

	inst := podToInstance(pod, "test-context")

	if inst.ID != "abc-123" {
		t.Errorf("podToInstance().ID = %q, want %q", inst.ID, "abc-123")
	}
	if inst.Name != "test-cluster-head" {
		t.Errorf("podToInstance().Name = %q, want %q", inst.Name, "test-cluster-head")
	}
	if inst.Region != "test-context" {
		t.Errorf("podToInstance().Region = %q, want %q", inst.Region, "test-context")
	}
	if inst.Status != cloud.StatusRunning {
		t.Errorf("podToInstance().Status = %q, want %q", inst.Status, cloud.StatusRunning)
	}
	if inst.PublicIP != "10.0.0.5" {
		t.Errorf("podToInstance().PublicIP = %q, want %q", inst.PublicIP, "10.0.0.5")
	}
	if inst.PrivateIP != "10.0.0.5" {
		t.Errorf("podToInstance().PrivateIP = %q, want %q", inst.PrivateIP, "10.0.0.5")
	}
}

func TestPodToInstance_Pending(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
			UID:  "def-456",
			Labels: map[string]string{
				labelClusterName: "test",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}

	inst := podToInstance(pod, "ctx")
	if inst.Status != cloud.StatusPending {
		t.Errorf("podToInstance().Status = %q, want %q", inst.Status, cloud.StatusPending)
	}
	if inst.PublicIP != "" {
		t.Errorf("podToInstance().PublicIP = %q, want empty", inst.PublicIP)
	}
}

func TestBuildPod(t *testing.T) {
	spec := &cloud.LaunchSpec{
		InstanceType: "4CPU--16GB",
		ImageID:      "ubuntu:22.04",
		NamePrefix:   "my-cluster",
		NumNodes:     1,
		Tags:         map[string]string{"env": "test"},
	}

	pod := buildPod(podParams{Name: "my-cluster-head", Namespace: "default", Spec: spec, Role: "head"})

	if pod.Name != "my-cluster-head" {
		t.Errorf("buildPod().Name = %q, want %q", pod.Name, "my-cluster-head")
	}
	if pod.Namespace != "default" {
		t.Errorf("buildPod().Namespace = %q, want %q", pod.Namespace, "default")
	}
	if len(pod.Spec.Containers) != 1 {
		t.Fatalf("buildPod().Containers = %d, want 1", len(pod.Spec.Containers))
	}
	if pod.Spec.Containers[0].Image != "ubuntu:22.04" {
		t.Errorf("buildPod().Containers[0].Image = %q, want %q", pod.Spec.Containers[0].Image, "ubuntu:22.04")
	}
	if pod.Labels[labelClusterName] != "my-cluster" {
		t.Errorf("buildPod().Labels[%s] = %q, want %q", labelClusterName, pod.Labels[labelClusterName], "my-cluster")
	}
	if pod.Labels[labelRole] != "head" {
		t.Errorf("buildPod().Labels[%s] = %q, want %q", labelRole, pod.Labels[labelRole], "head")
	}
	if pod.Labels["env"] != "test" {
		t.Errorf("buildPod().Labels[env] = %q, want %q", pod.Labels["env"], "test")
	}

	// Check resource requests
	res := pod.Spec.Containers[0].Resources
	cpuReq := res.Requests[corev1.ResourceCPU]
	if cpuReq.String() != "4" {
		t.Errorf("CPU request = %q, want %q", cpuReq.String(), "4")
	}
	memReq := res.Requests[corev1.ResourceMemory]
	if memReq.String() != "16Gi" {
		t.Errorf("Memory request = %q, want %q", memReq.String(), "16Gi")
	}
}

func TestBuildPod_WithGPU(t *testing.T) {
	spec := &cloud.LaunchSpec{
		InstanceType: "4CPU--16GB--H100:1",
		ImageID:      "nvidia/cuda:12.0",
		NamePrefix:   "gpu-cluster",
		NumNodes:     1,
		Region:       "test-context",
	}

	pod := buildPod(podParams{Name: "gpu-cluster-head", Namespace: "default", Spec: spec, Role: "head"})

	res := pod.Spec.Containers[0].Resources
	gpuReq := res.Requests[corev1.ResourceName("nvidia.com/gpu")]
	if gpuReq.String() != "1" {
		t.Errorf("GPU request = %q, want %q", gpuReq.String(), "1")
	}
}

func TestBuildPod_MultiNode(t *testing.T) {
	spec := &cloud.LaunchSpec{
		InstanceType: "8CPU--32GB",
		ImageID:      "ubuntu:22.04",
		NamePrefix:   "multi-cluster",
		NumNodes:     3,
	}

	// Head pod
	head := buildPod(podParams{Name: "multi-cluster-head", Namespace: "default", Spec: spec, Role: "head"})
	if head.Labels[labelRole] != "head" {
		t.Errorf("head pod role = %q, want %q", head.Labels[labelRole], "head")
	}

	// Worker pod joins the head's Ray cluster.
	worker := buildPod(podParams{Name: "multi-cluster-worker1", Namespace: "default", Spec: spec, Role: "worker", HeadAddr: "10.0.0.5"})
	if worker.Labels[labelRole] != "worker" {
		t.Errorf("worker pod role = %q, want %q", worker.Labels[labelRole], "worker")
	}
	if got := envValue(worker, envHeadAddr); got != "10.0.0.5" {
		t.Errorf("worker env[%s] = %q, want %q", envHeadAddr, got, "10.0.0.5")
	}
	if got := envValue(head, envHeadAddr); got != "" {
		t.Errorf("head env[%s] = %q, want empty", envHeadAddr, got)
	}

	// Head must bind Ray to its own pod IP; worker must join via head address.
	headCmd := head.Spec.Containers[0].Command
	if len(headCmd) < 3 || !strings.Contains(headCmd[2], "--head") {
		t.Errorf("head command = %v, want ray --head bootstrap", headCmd)
	}
	if !strings.Contains(headCmd[2], "--node-ip-address=$GPI_POD_IP") {
		t.Errorf("head command should inject pod IP: %v", headCmd)
	}
	if !strings.Contains(headCmd[2], gpiletBin) {
		t.Errorf("head command should start gpilet from %s: %v", gpiletBin, headCmd)
	}
	workerCmd := worker.Spec.Containers[0].Command
	if len(workerCmd) < 3 || !strings.Contains(workerCmd[2], "--address=$GPI_HEAD_ADDR:6379") {
		t.Errorf("worker command = %v, want ray --address bootstrap", workerCmd)
	}
}

func TestBuildPod_ConfigDrivenCommand(t *testing.T) {
	cfg := &Config{
		Namespace:        "ml",
		Image:            "example.com/custom:1",
		GpiletDir:        "/data/gpilet",
		GpiletInterval:   5,
		RayHeadPort:      7777,
		RayDashboardPort: 9999,
	}
	spec := &cloud.LaunchSpec{
		InstanceType: "4CPU--16GB",
		ImageID:      cfg.Image,
		NamePrefix:   "cfg-cluster",
		NumNodes:     2,
	}

	head := buildPod(podParams{Name: "cfg-cluster-head", Namespace: "default", Spec: spec, Role: "head", Cfg: cfg})
	headCmd := head.Spec.Containers[0].Command
	if !strings.Contains(headCmd[2], "--dir /data/gpilet --interval 5") {
		t.Errorf("head command should use configured gpilet dir/interval: %v", headCmd)
	}
	if !strings.Contains(headCmd[2], "--port=7777") || !strings.Contains(headCmd[2], "--dashboard-port=9999") {
		t.Errorf("head command should use configured Ray ports: %v", headCmd)
	}

	worker := buildPod(podParams{Name: "cfg-cluster-worker1", Namespace: "default", Spec: spec, Role: "worker", HeadAddr: "10.0.0.9", Cfg: cfg})
	workerCmd := worker.Spec.Containers[0].Command
	if !strings.Contains(workerCmd[2], "--address=$GPI_HEAD_ADDR:7777") {
		t.Errorf("worker command should use configured Ray head port: %v", workerCmd)
	}
}

func TestConfig_EffectiveDefaults(t *testing.T) {
	if got := (*Config)(nil).EffectiveNamespace(); got != defaultNamespace {
		t.Errorf("nil EffectiveNamespace = %q, want %q", got, defaultNamespace)
	}
	if got := (&Config{}).EffectiveNamespace(); got != defaultNamespace {
		t.Errorf("empty EffectiveNamespace = %q, want %q", got, defaultNamespace)
	}
	if got := (&Config{Namespace: "x"}).EffectiveNamespace(); got != "x" {
		t.Errorf("EffectiveNamespace = %q, want x", got)
	}
	if got := (&Config{GpiletInterval: 3}).EffectiveGpiletInterval(); got != 3 {
		t.Errorf("EffectiveGpiletInterval = %d, want 3", got)
	}
	if got := (&Config{}).EffectiveGpiletInterval(); got != gpiletIntervalSec {
		t.Errorf("empty EffectiveGpiletInterval = %d, want %d", got, gpiletIntervalSec)
	}
	if got := (&Config{RayHeadPort: 7000}).EffectiveRayHeadPort(); got != 7000 {
		t.Errorf("EffectiveRayHeadPort = %d, want 7000", got)
	}
	if got := (&Config{}).EffectiveRayHeadPort(); got != rayHeadPort {
		t.Errorf("empty EffectiveRayHeadPort = %d, want %d", got, rayHeadPort)
	}
}

func TestGetImage_Default(t *testing.T) {
	p := Provider{}
	img, err := p.GetImage(context.Background(), "ctx", "")
	if err != nil {
		t.Fatalf("GetImage: %v", err)
	}
	if img != defaultBaseImage {
		t.Errorf("GetImage() = %q, want default %q", img, defaultBaseImage)
	}
}

func TestGetImage_ConfigOverride(t *testing.T) {
	config.SetPath(filepath.Join(t.TempDir(), "config.yaml"))
	defer config.Reset()
	cfgPath, _ := config.UserPath()
	if err := os.WriteFile(cfgPath, []byte("kubernetes:\n  image: example.com/custom:1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	p := Provider{}
	img, err := p.GetImage(context.Background(), "ctx", "")
	if err != nil {
		t.Fatalf("GetImage: %v", err)
	}
	if img != "example.com/custom:1" {
		t.Errorf("GetImage() = %q, want config override", img)
	}
}

func TestCloudName(t *testing.T) {
	p := Provider{}
	if p.Name() != "kubernetes" {
		t.Errorf("Provider.Name() = %q, want %q", p.Name(), "kubernetes")
	}
}

func TestTruncateName(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"short", 10, "short"},
		{"a-very-long-cluster-name-that-exceeds-limit", 12, "a-very-long-"},
		{"exact", 5, "exact"},
		{"", 10, ""},
	}
	for _, tt := range tests {
		got := truncateName(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("truncateName(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}

func TestBuildPod_NoGPU_EnvVars(t *testing.T) {
	spec := &cloud.LaunchSpec{
		InstanceType: "4CPU--16GB",
		ImageID:      "ubuntu:22.04",
		NamePrefix:   "test",
		NumNodes:     1,
	}

	pod := buildPod(podParams{Name: "test-head", Namespace: "default", Spec: spec, Role: "head"})

	// Should have NVIDIA_VISIBLE_DEVICES=none when no GPU
	found := false
	for _, env := range pod.Spec.Containers[0].Env {
		if env.Name == "NVIDIA_VISIBLE_DEVICES" && env.Value == "none" {
			found = true
			break
		}
	}
	if !found {
		t.Error("buildPod() without GPU should set NVIDIA_VISIBLE_DEVICES=none")
	}

	// Head should inject its own pod IP via downward API for `ray start --head`.
	podIPSet := false
	for _, env := range pod.Spec.Containers[0].Env {
		if env.Name == envPodIP && env.ValueFrom != nil && env.ValueFrom.FieldRef != nil &&
			env.ValueFrom.FieldRef.FieldPath == "status.podIP" {
			podIPSet = true
			break
		}
	}
	if !podIPSet {
		t.Error("buildPod() head should inject GPI_POD_IP via downward API")
	}
}

func TestBuildPod_WithGPU_NoNvidiaEnv(t *testing.T) {
	spec := &cloud.LaunchSpec{
		InstanceType: "4CPU--16GB--H100:1",
		ImageID:      "nvidia/cuda:12.0",
		NamePrefix:   "gpu-test",
		NumNodes:     1,
		Region:       "test-context",
	}

	pod := buildPod(podParams{Name: "gpu-test-head", Namespace: "default", Spec: spec, Role: "head"})

	// Should NOT have NVIDIA_VISIBLE_DEVICES=none when GPU is requested
	for _, env := range pod.Spec.Containers[0].Env {
		if env.Name == "NVIDIA_VISIBLE_DEVICES" {
			t.Errorf("buildPod() with GPU should not set NVIDIA_VISIBLE_DEVICES, got %q", env.Value)
		}
	}
}

func TestBuildPod_RestartPolicy(t *testing.T) {
	spec := &cloud.LaunchSpec{
		InstanceType: "4CPU--16GB",
		ImageID:      "ubuntu:22.04",
		NamePrefix:   "test",
		NumNodes:     1,
	}

	pod := buildPod(podParams{Name: "test-head", Namespace: "default", Spec: spec, Role: "head"})
	if pod.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("RestartPolicy = %q, want %q", pod.Spec.RestartPolicy, corev1.RestartPolicyNever)
	}
}

func TestBuildPod_LabelsComplete(t *testing.T) {
	spec := &cloud.LaunchSpec{
		InstanceType: "4CPU--16GB",
		ImageID:      "ubuntu:22.04",
		NamePrefix:   "my-cluster",
		NumNodes:     1,
		Tags:         map[string]string{"team": "ml"},
	}

	pod := buildPod(podParams{Name: "my-cluster-head", Namespace: "default", Spec: spec, Role: "head"})

	if pod.Labels[labelClusterName] != "my-cluster" {
		t.Errorf("Labels[%s] = %q, want %q", labelClusterName, pod.Labels[labelClusterName], "my-cluster")
	}
	if pod.Labels[labelRole] != "head" {
		t.Errorf("Labels[%s] = %q, want %q", labelRole, pod.Labels[labelRole], "head")
	}
	if pod.Labels[labelManagedBy] != "gpi" {
		t.Errorf("Labels[%s] = %q, want %q", labelManagedBy, pod.Labels[labelManagedBy], "gpi")
	}
	if pod.Labels["team"] != "ml" {
		t.Errorf("Labels[team] = %q, want %q", pod.Labels["team"], "ml")
	}
}

func TestBuildPod_MemoryOnly(t *testing.T) {
	spec := &cloud.LaunchSpec{
		InstanceType: "2CPU--4GB",
		ImageID:      "ubuntu:22.04",
		NamePrefix:   "small",
		NumNodes:     1,
	}

	pod := buildPod(podParams{Name: "small-head", Namespace: "default", Spec: spec, Role: "head"})

	res := pod.Spec.Containers[0].Resources
	cpuReq := res.Requests[corev1.ResourceCPU]
	if cpuReq.String() != "2" {
		t.Errorf("CPU request = %q, want %q", cpuReq.String(), "2")
	}
	memReq := res.Requests[corev1.ResourceMemory]
	if memReq.String() != "4Gi" {
		t.Errorf("Memory request = %q, want %q", memReq.String(), "4Gi")
	}
}

func TestBuildPod_GPUNvidiaResourceKey(t *testing.T) {
	spec := &cloud.LaunchSpec{
		InstanceType: "4CPU--16GB--A100:2",
		ImageID:      "nvidia/cuda:12.0",
		NamePrefix:   "gpu",
		NumNodes:     1,
		Region:       "test",
	}

	pod := buildPod(podParams{Name: "gpu-head", Namespace: "default", Spec: spec, Role: "head"})
	res := pod.Spec.Containers[0].Resources
	gpuReq := res.Requests[corev1.ResourceName("nvidia.com/gpu")]
	if gpuReq.String() != "2" {
		t.Errorf("GPU request = %q, want %q", gpuReq.String(), "2")
	}
}

func TestBuildPod_TPUResourceKey(t *testing.T) {
	spec := &cloud.LaunchSpec{
		InstanceType: "8CPU--32GB--TPU-V4:1",
		ImageID:      "google/cloud-tpu:latest",
		NamePrefix:   "tpu",
		NumNodes:     1,
		Region:       "test",
	}

	pod := buildPod(podParams{Name: "tpu-head", Namespace: "default", Spec: spec, Role: "head"})
	res := pod.Spec.Containers[0].Resources
	tpuReq := res.Requests[corev1.ResourceName("google.com/tpu")]
	if tpuReq.String() != "1" {
		t.Errorf("TPU request = %q, want %q", tpuReq.String(), "1")
	}
}
