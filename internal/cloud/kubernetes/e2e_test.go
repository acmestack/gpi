//go:build e2e

// Package kubernetes e2e tests exercise the Provider against a real
// Kubernetes cluster (e.g. kind). They are excluded from the normal unit-test
// run via the `e2e` build tag and require a reachable kubeconfig
// (KUBECONFIG env or ~/.kube/config).
package kubernetes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/acmestack/gpi/internal/cloud"
	"github.com/acmestack/gpi/internal/gpilet"
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
	// RunInstances now waits for the pod to reach Running before returning.
	if inst.Status != cloud.StatusRunning {
		t.Errorf("instance status = %q, want %q", inst.Status, cloud.StatusRunning)
	}

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
	// Pod Running does not imply Ray is ready: the bootstrap script runs
	// `ray start` after the container starts, and the worker needs time to
	// join, so poll `ray status` until the cluster shows the expected nodes.
	headPod := insts[0].Name
	headPodObj, err := cs.CoreV1().Pods(defaultNamespace).Get(ctx, headPod, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get head pod %s: %v", headPod, err)
	}
	if headPodObj.Status.PodIP == "" {
		t.Fatalf("head pod %s has no PodIP", headPod)
	}
	headAddr := headPodObj.Status.PodIP + ":" + strconv.Itoa(rayHeadPort)
	rayStatus := pollRayStatus(t, ctx, cs, headPod, headAddr, 120*time.Second)
	// A "Node status" block proves `ray status` reached the GCS and the
	// runtime is up. (The node lines are labeled "Healthy:" or "Active:"
	// depending on the Ray version, so don't match on that keyword.) The
	// two-node check is done below via a raylet process in both pods.
	if !strings.Contains(rayStatus, "Node status") {
		dumpPodDiagnostics(t, ctx, region, headPod)
		t.Errorf("head ray status did not report the cluster:\n%s", rayStatus)
	} else {
		t.Logf("head ray status:\n%s", rayStatus)
	}

	// A 2-node cluster means a raylet process is running in BOTH pods (head
	// and worker), not just a healthy head. Poll each pod's raylet.
	for _, pod := range []string{headPod, insts[1].Name} {
		var rl string
		rlDeadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(rlDeadline) {
			rl = execInPod(t, ctx, cs, pod, "pgrep -f raylet")
			if strings.TrimSpace(rl) != "" {
				break
			}
			time.Sleep(2 * time.Second)
		}
		if strings.TrimSpace(rl) == "" {
			if pod != headPod {
				dumpPodDiagnostics(t, ctx, region, pod)
				// The worker retries `ray start --address=...`; its stdout
				// shows the join error. Log the last worker logs.
				workerLog := execInPod(t, ctx, cs, pod, "tail -n 30 /tmp/ray/session_latest/logs/worker_start.log 2>/dev/null || tail -n 30 /tmp/ray/session_latest/logs/raylet.out 2>/dev/null || echo 'no ray logs'")
				t.Logf("worker ray join logs:\n%s", workerLog)
			}
			t.Errorf("raylet process not found in pod %s (worker did not join the Ray cluster)", pod)
		}
	}

	// Count registered nodes from `ray status`: every Active node shows as a
	// "node_<hex>" line. A worker can register with the GCS and then take a
	// moment to become schedulable, so wait for BOTH nodes to appear before
	// running the functional task.
	activeNodes := func() int {
		out := execInPod(t, ctx, cs, headPod, "ray status --address="+headAddr+" 2>&1")
		n := 0
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "node_") {
				n++
			}
		}
		return n
	}
	regDeadline := time.Now().Add(90 * time.Second)
	for activeNodes() < 2 && time.Now().Before(regDeadline) {
		time.Sleep(2 * time.Second)
	}
	t.Logf("ray status reports %d registered node(s)", activeNodes())

	// Run a REAL distributed Ray task and require it to execute on BOTH
	// nodes. `ray status` + a raylet process only prove the cluster is up;
	// scheduling a @ray.remote function exercises the driver, the scheduler,
	// the object store and worker registration end to end. Retry a few times
	// because a freshly-joined worker may not yet be schedulable.
	taskScript := `cat > /tmp/ray_dist_task.py <<'PYEOF'
import ray
ray.init(address="` + headAddr + `")

@ray.remote
def node_ip():
    import ray.util
    return ray.util.get_node_ip_address()

addrs = ray.get([node_ip.remote() for _ in range(8)])
print("NODE_IPS=" + ",".join(sorted(set(addrs))))
PYEOF
python /tmp/ray_dist_task.py`
	var taskOut string
	nodes := map[string]bool{}
	for attempt := 1; attempt <= 4; attempt++ {
		taskOut = execInPod(t, ctx, cs, headPod, taskScript)
		nodes = map[string]bool{}
		if _, after, ok := strings.Cut(taskOut, "NODE_IPS="); ok {
			if nl := strings.IndexByte(after, '\n'); nl >= 0 {
				after = after[:nl]
			}
			for _, ip := range strings.Split(after, ",") {
				if ip = strings.TrimSpace(ip); ip != "" {
					nodes[ip] = true
				}
			}
		}
		if len(nodes) >= 2 {
			break
		}
		t.Logf("distributed Ray task attempt %d: %d node(s) %v, retrying in 5s", attempt, len(nodes), nodes)
		time.Sleep(5 * time.Second)
	}
	if len(nodes) < 2 {
		dumpPodDiagnostics(t, ctx, region, headPod)
		t.Errorf("distributed Ray task did not execute on 2 nodes; got %d node(s) %v:\n%s", len(nodes), nodes, taskOut)
	} else {
		t.Logf("distributed Ray task ran on %d nodes: %v", len(nodes), nodes)
	}

	// gpilet must be running inside both pods (nohup background start, so
	// poll briefly in case it is still spawning).
	pods := []string{headPod, insts[1].Name}
	for _, pod := range pods {
		var pout string
		pdeadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(pdeadline) {
			pout = execInPod(t, ctx, cs, pod, "pgrep -f /usr/local/bin/gpilet")
			if strings.TrimSpace(pout) != "" {
				break
			}
			time.Sleep(2 * time.Second)
		}
		if strings.TrimSpace(pout) == "" {
			t.Errorf("gpilet process not found in pod %s", pod)
		}
	}

	// gpilet must have written a VALID status file in both pods (default
	// interval 10s). Parse it and verify the fields gpilet is responsible
	// for collecting, so a silently-broken Collect() cannot pass.
	for _, pod := range pods {
		var st *gpilet.Status
		statusDeadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(statusDeadline) {
			statusOut := execInPod(t, ctx, cs, pod, "cat /var/lib/gpilet/status.json 2>/dev/null")
			if strings.TrimSpace(statusOut) == "" {
				time.Sleep(2 * time.Second)
				continue
			}
			if err := json.Unmarshal([]byte(statusOut), &st); err != nil {
				t.Errorf("gpilet status.json in pod %s is not valid JSON: %v\n%s", pod, err, statusOut)
			}
			break
		}
		if st == nil {
			t.Errorf("gpilet status.json not found in pod %s", pod)
			continue
		}
		t.Logf("gpilet status in pod %s: hostname=%q cpus=%d mem_total_gb=%.1f ray_running=%v collected_at=%v",
			pod, st.Hostname, st.CPUs, st.MemTotalGB, st.RayRunning, st.CollectedAt)
		if st.Hostname != pod {
			t.Errorf("gpilet status in pod %s: hostname = %q, want %q", pod, st.Hostname, pod)
		}
		if st.CPUs < 1 {
			t.Errorf("gpilet status in pod %s: cpus = %d, want >= 1", pod, st.CPUs)
		}
		if st.MemTotalGB <= 0 {
			t.Errorf("gpilet status in pod %s: mem_total_gb = %v, want > 0", pod, st.MemTotalGB)
		}
		if !st.RayRunning {
			t.Errorf("gpilet status in pod %s: ray_running = false, want true", pod)
		}
		if age := time.Since(st.CollectedAt); age > 30*time.Second {
			t.Errorf("gpilet status in pod %s: collected_at %v is %v old, want fresh", pod, st.CollectedAt, age)
		}
	}
}

// pollRayStatus repeatedly runs `ray status` in the head pod until it reaches
// the GCS and reports a running runtime, or a timeout elapses. The GCS may
// bind either the head pod IP (--node-ip-address) or loopback depending on
// Ray's interface detection, so both explicit forms are tried. Ray CLI
// failures (e.g. "Failed to connect to GCS ... within 5 seconds") go to
// stderr, which execInPod merges into the returned output. It returns the
// last output for diagnostics.
func pollRayStatus(t *testing.T, ctx context.Context, cs *kubernetes.Clientset, podName, headAddr string, timeout time.Duration) string {
	t.Helper()
	_, headPort, _ := strings.Cut(headAddr, ":")
	cmd := "ray status --address=" + headAddr + " 2>&1; echo '=== 127.0.0.1 ==='; ray status --address=127.0.0.1:" + headPort + " 2>&1"
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		out := execInPod(t, ctx, cs, podName, cmd)
		last = out
		// "Node status" is present in every version of `ray status` output
		// once it reaches the GCS. ("Ray runtime is running" is only printed
		// by `ray start`; node lines are labeled "Healthy:" or "Active:".)
		if strings.Contains(out, "Node status") {
			return out
		}
		time.Sleep(2 * time.Second)
	}
	logGCSConnectivity(t, ctx, cs, podName, headAddr)
	return last
}

// logGCSConnectivity dumps the head's GCS bind address and whether the GCS
// port is reachable, so a failing `ray status` is actionable.
func logGCSConnectivity(t *testing.T, ctx context.Context, cs *kubernetes.Clientset, podName, headAddr string) {
	t.Helper()
	headIP, headPort, _ := strings.Cut(headAddr, ":")
	t.Logf("ray GCS connectivity diagnostics (pod %s, addr %s):", podName, headAddr)
	probes := []string{
		"cat /tmp/ray/current_cluster 2>&1 || true",
		"cat /tmp/ray/session_latest/ray_node_ip_address 2>&1 || true",
		"python3 -c \"import socket;s=socket.create_connection(('127.0.0.1'," + headPort + "),2);print('loopback " + headPort + " reachable')\" 2>/dev/null || python -c \"import socket;s=socket.create_connection(('127.0.0.1'," + headPort + "),2);print('loopback " + headPort + " reachable')\" 2>/dev/null || echo 'loopback " + headPort + " unreachable'",
		"python3 -c \"import socket;s=socket.create_connection(('" + headIP + "'," + headPort + "),2);print('" + headIP + " " + headPort + " reachable')\" 2>/dev/null || python -c \"import socket;s=socket.create_connection(('" + headIP + "'," + headPort + "),2);print('" + headIP + " " + headPort + " reachable')\" 2>/dev/null || echo '" + headIP + " " + headPort + " unreachable'",
		"tail -n 15 /tmp/ray/session_latest/logs/gcs_server.out 2>&1 || true",
	}
	for _, c := range probes {
		t.Logf("$ %s\n%s", c, execInPod(t, ctx, cs, podName, c))
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
		t.Logf("exec %s: %v", podName, err)
		return ""
	}
	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		// A non-zero exit (e.g. `ray status` before the runtime is up) is not
		// a test failure by itself; callers decide what to assert on.
		t.Logf("exec %s (%q) failed: %v; stderr: %s", podName, cmd, err, stderr.String())
	}
	// Merge stderr into the result so partial/missing output is visible to
	// callers (and pollRayStatus diagnostics) instead of silently dropping it.
	return strings.TrimSpace(stdout.String() + "\n" + stderr.String())
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
