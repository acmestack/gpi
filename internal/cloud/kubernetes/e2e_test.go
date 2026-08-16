//go:build e2e

// Package kubernetes e2e tests exercise the Provider against a real
// Kubernetes cluster (e.g. kind). They are excluded from the normal unit-test
// run via the `e2e` build tag and require a reachable kubeconfig
// (KUBECONFIG env or ~/.kube/config).
package kubernetes

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/acmestack/gpi/internal/cloud"
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
	t.Fatalf("pod %s did not reach Running within 60s", id)
}

func cloudLaunchSpec(prefix, region string) *cloud.LaunchSpec {
	image := os.Getenv("GPI_E2E_IMAGE")
	if image == "" {
		// registry.k8s.io/pause is preloaded in every kind node image and
		// stays Running, so it is the most reliable choice for the lifecycle
		// test. Override with GPI_E2E_IMAGE when a custom image is needed.
		image = "registry.k8s.io/pause:3.9"
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