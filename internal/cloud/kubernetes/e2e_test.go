//go:build e2e

// Package kubernetes e2e tests exercise the Provider against a real
// Kubernetes cluster (e.g. kind). They are excluded from the normal unit-test
// run via the `e2e` build tag and require a reachable kubeconfig
// (KUBECONFIG env or ~/.kube/config).
package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acmestack/gpi/internal/cloud"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/pointer"
)

// TestE2ELifecycle creates a single-node pod, lists it back, describes it,
// fetches its IP, and finally terminates it. It exercises the real K8s API.
func TestE2ELifecycle(t *testing.T) {
	ctx := context.Background()

	if _, err := CurrentContext(); err != nil {
		t.Skipf("no kubeconfig available, skipping e2e: %v", err)
	}

	region, err := CurrentContext()
	if err != nil {
		t.Fatalf("CurrentContext: %v", err)
	}

	prefix := "gpi-e2e-" + randomSuffix()
	cleanup := true
	defer func() {
		if !cleanup {
			return
		}
		p := Provider{}
		insts, err := p.ListInstances(ctx, region, prefix)
		if err != nil {
			return
		}
		ids := make([]string, 0, len(insts))
		for _, in := range insts {
			ids = append(ids, in.ID)
		}
		_ = p.TerminateInstances(ctx, region, ids)
	}()

	p := Provider{}
	spec := cloudLaunchSpec(prefix, region)
	insts, err := p.RunInstances(ctx, spec)
	if err != nil {
		t.Fatalf("RunInstances: %v", err)
	}
	if len(insts) != 1 {
		t.Fatalf("RunInstances returned %d instances, want 1", len(insts))
	}
	inst := insts[0]
	if inst.Status != cloud.StatusPending {
		t.Errorf("instance status = %q, want %q", inst.Status, cloud.StatusPending)
	}

	// Wait for the pod to reach Running.
	waitForPodRunning(t, ctx, p, region, inst.ID)

	// List by prefix.
	listed, err := p.ListInstances(ctx, region, prefix)
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("ListInstances returned %d instances, want 1", len(listed))
	}
	if listed[0].Status != cloud.StatusRunning {
		t.Errorf("listed status = %q, want %q", listed[0].Status, cloud.StatusRunning)
	}

	// Describe by ID.
	described, err := p.DescribeInstances(ctx, region, []string{inst.ID})
	if err != nil {
		t.Fatalf("DescribeInstances: %v", err)
	}
	if len(described) != 1 {
		t.Fatalf("DescribeInstances returned %d instances, want 1", len(described))
	}

	// Public IP should be the pod IP (non-empty once Running).
	ip, err := p.GetPublicIP(ctx, region, inst.Name)
	if err != nil {
		t.Fatalf("GetPublicIP: %v", err)
	}
	if ip == "" {
		t.Errorf("GetPublicIP returned empty IP for running pod")
	}

	// Terminate and confirm gone.
	if err := p.TerminateInstances(ctx, region, []string{inst.Name}); err != nil {
		t.Fatalf("TerminateInstances: %v", err)
	}
	cleanup = false
	remaining, err := p.ListInstances(ctx, region, prefix)
	if err != nil {
		t.Fatalf("ListInstances after terminate: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected 0 remaining pods after terminate, got %d", len(remaining))
	}
}

// TestE2EGpiletAndRay creates a 2-node cluster and verifies that the
// SkyPilot-style bootstrap actually works: gpilet runs in every node pod and
// a Ray cluster forms with the head and the worker.
func TestE2EGpiletAndRay(t *testing.T) {
	ctx := context.Background()

	if _, err := CurrentContext(); err != nil {
		t.Skipf("no kubeconfig available, skipping e2e: %v", err)
	}
	region, err := CurrentContext()
	if err != nil {
		t.Fatalf("CurrentContext: %v", err)
	}

	prefix := "gpi-e2e-" + randomSuffix()
	defer func() {
		p := Provider{}
		insts, err := p.ListInstances(ctx, region, prefix)
		if err != nil {
			return
		}
		ids := make([]string, 0, len(insts))
		for _, in := range insts {
			ids = append(ids, in.ID)
		}
		_ = p.TerminateInstances(ctx, region, ids)
	}()

	spec := cloudLaunchSpec(prefix, region)
	spec.NumNodes = 2
	p := Provider{}
	insts, err := p.RunInstances(ctx, spec)
	if err != nil {
		t.Fatalf("RunInstances: %v", err)
	}
	if len(insts) != 2 {
		t.Fatalf("RunInstances returned %d instances, want 2", len(insts))
	}

	cs, err := clientFor(region)
	if err != nil {
		t.Fatalf("clientFor: %v", err)
	}

	// Wait for both pods to be Running.
	for _, in := range insts {
		waitForPodRunning(t, ctx, p, region, in.ID)
	}

	// Head must show a 2-node Ray cluster (head + worker). The first
	// instance returned by RunInstances is the head; the rest are workers.
	headPod := insts[0].Name
	rayStatus := execInPod(t, ctx, cs, headPod, "ray status")
	if !strings.Contains(rayStatus, "Ray runtime is running") {
		t.Errorf("head ray status did not report a running runtime:\n%s", rayStatus)
	}
	if !strings.Contains(rayStatus, "2 nodes") && !strings.Contains(rayStatus, "1 active, 1 pending") {
		t.Errorf("head ray status does not show the worker joined:\n%s", rayStatus)
	}

	// gpilet must be running inside both pods.
	pods := []string{headPod, insts[1].Name}
	for _, pod := range pods {
		out := execInPod(t, ctx, cs, pod, "pgrep -f /usr/local/bin/gpilet")
		if strings.TrimSpace(out) == "" {
			t.Errorf("gpilet process not found in pod %s", pod)
		}
	}

	// gpilet must have written its status file (default interval 10s).
	out := execInPod(t, ctx, cs, headPod, "test -f /var/lib/gpilet/status.json && cat /var/lib/gpilet/status.json")
	if strings.TrimSpace(out) == "" {
		t.Errorf("gpilet status.json not found in pod %s", headPod)
	}
}

// execInPod runs a command in the first container of a pod and returns stdout.
func execInPod(t *testing.T, ctx context.Context, cs *kubernetes.Clientset, podName, cmd string) string {
	t.Helper()
	cfg := restConfig()
	req := cs.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(podName).
		Namespace(defaultNamespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: "ray-node",
			Command:   []string{"/bin/sh", "-c", cmd},
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(cfg, "POST", req.URL())
	if err != nil {
		t.Fatalf("exec %s: %v", podName, err)
	}
	var buf bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &buf,
		Stderr: &buf,
	})
	if err != nil {
		t.Fatalf("exec %s (%q): %v", podName, cmd, err)
	}
	return buf.String()
}

// restConfig returns the REST config for the current context (used by exec).
func restConfig() *rest.Config {
	cfg := os.Getenv("KUBECONFIG")
	if cfg == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		cfg = filepath.Join(home, ".kube", "config")
	}
	rc, err := clientcmd.BuildConfigFromFlags("", cfg)
	if err != nil {
		return nil
	}
	return rc
}

// waitForPodRunning polls ListInstances until the pod is running or a timeout.
func waitForPodRunning(t *testing.T, ctx context.Context, p Provider, region, id string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		insts, err := p.DescribeInstances(ctx, region, []string{id})
		if err == nil && len(insts) == 1 && insts[0].Status == cloud.StatusRunning {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	dumpPodDiagnostics(t, ctx, region, id)
	t.Fatalf("pod %s did not reach Running within 60s", id)
}

// dumpPodDiagnostics prints pod status, recent events and container logs for a
// non-running pod, so a failed e2e leaves actionable information in CI logs.
func dumpPodDiagnostics(t *testing.T, ctx context.Context, region, id string) {
	t.Helper()
	cs, err := clientFor(region)
	if err != nil {
		t.Logf("diagnostics: clientFor: %v", err)
		return
	}
	ns := defaultNamespace
	if c := LoadConfig(); c != nil {
		ns = c.EffectiveNamespace()
	}
	pod, err := cs.CoreV1().Pods(ns).Get(ctx, id, metav1.GetOptions{})
	if err != nil {
		// id may be the pod name or UID; fall back to listing by prefix is
		// complex here, so log the raw error.
		t.Logf("diagnostics: get pod %s: %v", id, err)
		return
	}
	t.Logf("diagnostics: pod %s phase=%s", pod.Name, pod.Status.Phase)
	for _, c := range pod.Status.ContainerStatuses {
		t.Logf("diagnostics: container %s ready=%v restart=%d state=%+v lastTerm=%+v",
			c.Name, c.Ready, c.RestartCount, c.State, c.LastTerminationState)
	}

	// Recent events.
	evs, _ := cs.CoreV1().Events(ns).List(ctx, metav1.ListOptions{
		FieldSelector: "involvedObject.name=" + pod.Name,
	})
	for _, e := range evs.Items {
		t.Logf("diagnostics: event %s: %s", e.Reason, e.Message)
	}

	// Container logs (last 40 lines) - ImagePullBackOff / CrashLoopBackOff
	// causes show up here.
	opts := &corev1.PodLogOptions{TailLines: pointer.Int64(40), Previous: false}
	logs, _ := cs.CoreV1().Pods(ns).GetLogs(pod.Name, opts).Do(ctx).Raw()
	if len(logs) > 0 {
		t.Logf("diagnostics: pod logs:\n%s", logs)
	}
	prev := &corev1.PodLogOptions{TailLines: pointer.Int64(40), Previous: true}
	if prevLogs, err := cs.CoreV1().Pods(ns).GetLogs(pod.Name, prev).Do(ctx).Raw(); err == nil && len(prevLogs) > 0 {
		t.Logf("diagnostics: previous pod logs:\n%s", prevLogs)
	}
}

func cloudLaunchSpec(prefix, region string) *cloud.LaunchSpec {
	image := os.Getenv("GPI_E2E_IMAGE")
	if image == "" {
		// Default to the gpi base image (gpilet + Ray) so e2e verifies real
		// runtime bootstrap. Override with GPI_E2E_IMAGE when needed.
		image = defaultBaseImage
	}
	return &cloud.LaunchSpec{
		NamePrefix:   prefix,
		Region:       region,
		NumNodes:     1,
		InstanceType: "1CPU--1GB",
		ImageID:      image,
		Tags: map[string]string{
			"purpose": "e2e-test",
		},
	}
}

func randomSuffix() string {
	now := time.Now()
	return fmt.Sprintf("%d%02d%02d%d", now.Unix(), now.Nanosecond()%97, time.Now().UnixNano()%89, os.Getpid())
}
