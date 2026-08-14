package provisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/acmestack/gpi/internal/cloud"
	"github.com/acmestack/gpi/internal/gpilet"
	"github.com/acmestack/gpi/internal/logging"
	"github.com/acmestack/gpi/internal/optimizer"
	"github.com/acmestack/gpi/internal/state"
	"github.com/acmestack/gpi/internal/task"
)

// logger is the package logger, tagged with the module name.
var logger = logging.WithName("provisioner")

// Provisioner orchestrates the full cloud lifecycle: keypair + image
// resolution, instance launch, Ray bootstrap, gpilet deployment, task
// execution and cluster teardown.
type Provisioner struct {
	Store *state.Store
	Dir   string
}

// New returns a Provisioner backed by the given store and GPI_HOME dir.
func New(store *state.Store, dir string) *Provisioner {
	return &Provisioner{Store: store, Dir: dir}
}

// Launch provisions a cluster for a task: resolves the keypair and image,
// runs instances, waits until running, bootstraps Ray (multi-node), deploys
// gpilet, and records the cluster in the store.
func (p *Provisioner) Launch(ctx context.Context, name string, ts *task.Task, l *optimizer.Launch) (*state.Cluster, error) {
	logger.Info("launching cluster",
		"cluster", name,
		"cloud", l.Cloud,
		"region", l.Region,
		"instance", l.InstanceType,
		"nodes", l.NumNodes)
	prov, err := cloud.New(l.Cloud, toCloudCreds(ts.Credentials.ForCloud(l.Cloud)))
	if err != nil {
		return nil, err
	}
	if prov == nil {
		return nil, fmt.Errorf("cloud provider %q not registered (available: %s)", l.Cloud, strings.Join(cloud.Names(), ", "))
	}

	keyName, keyPath, err := p.ensureKeyPair(ctx, prov, name, l.Cloud, l.Region)
	if err != nil {
		return nil, err
	}

	imageID, err := prov.GetImage(ctx, l.Region, "ubuntu")
	if err != nil {
		return nil, err
	}

	diskGiB := 0
	if ts.Resources.DiskSize != nil && ts.Resources.DiskSize.Min != nil {
		diskGiB = int(*ts.Resources.DiskSize.Min)
	}

	tags := map[string]string{
		"gpi:cluster": name,
		"gpi:cloud":   l.Cloud,
	}
	for k, v := range ts.Tags {
		tags[k] = v
	}
	if ts.Resources != nil {
		for k, v := range ts.Resources.Labels {
			if _, exists := tags[k]; !exists {
				tags[k] = v
			}
		}
	}

	spec := &cloud.LaunchSpec{
		InstanceType:       l.InstanceType,
		Region:             l.Region,
		Zone:               l.Zone,
		NumNodes:           l.NumNodes,
		ImageID:            imageID,
		KeyName:            keyName,
		DiskSizeGiB:        diskGiB,
		NamePrefix:         name,
		Tags:               tags,
		UserData:           bootstrapScript(name),
		ResumeStoppedNodes: true,
	}
	if l.UseSpot {
		spec.SpotStrategy = "SpotAsPriceGo"
	}

	instances, err := prov.RunInstances(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("run instances: %w", err)
	}
	logger.Info("instances running",
		"cluster", name,
		"count", len(instances))

	nodes := make([]state.Node, 0, len(instances))
	for i, inst := range instances {
		node := state.Node{
			ID:           inst.ID,
			InstanceType: inst.InstanceType,
			Zone:         inst.Zone,
			Status:       string(inst.Status),
		}
		if len(instances) > 1 {
			if i == 0 {
				node.Role = state.RoleHead
			} else {
				node.Role = state.RoleWorker
			}
		}
		nodes = append(nodes, node)
	}

	cluster := &state.Cluster{
		Name:       name,
		Status:     state.ClusterProvisioning,
		Cloud:      l.Cloud,
		Region:     l.Region,
		NumNodes:   l.NumNodes,
		Instances:  nodes,
		Launch:     l,
		TaskYAML:   taskYAML(ts),
		Tags:       cloneMap(tags),
		CloudCreds: stateCloudCreds(ts.Credentials.ForCloud(l.Cloud)),
		KeyName:    keyName,
		KeyPath:    keyPath,
	}
	if ts.Resources != nil {
		cluster.Labels = cloneMap(ts.Resources.Labels)
	}
	if err := p.Store.AddCluster(cluster); err != nil {
		return nil, err
	}

	if err := p.waitReady(ctx, prov, cluster); err != nil {
		p.Store.UpdateCluster(name, func(c *state.Cluster) error {
			c.Status = state.ClusterError
			c.LastError = err.Error()
			return nil
		})
		p.recordClusterEvent(name, state.ClusterProvisioning, state.ClusterError, state.EventStatusChange, err.Error())
		return nil, err
	}

	if l.NumNodes > 1 {
		if err := p.bootstrapRay(ctx, cluster, ts); err != nil {
			p.Store.UpdateCluster(name, func(c *state.Cluster) error {
				c.Status = state.ClusterError
				c.LastError = "ray bootstrap: " + err.Error()
				return nil
			})
			p.recordClusterEvent(name, state.ClusterProvisioning, state.ClusterError, state.EventStatusChange, "ray bootstrap: "+err.Error())
			return nil, err
		}
	}

	if err := p.installGpilet(ctx, cluster); err != nil {
		p.Store.UpdateCluster(name, func(c *state.Cluster) error {
			c.Status = state.ClusterError
			c.LastError = "gpilet install: " + err.Error()
			return nil
		})
		p.recordClusterEvent(name, state.ClusterProvisioning, state.ClusterError, state.EventStatusChange, "gpilet install: "+err.Error())
		return nil, err
	}
	logger.Info("cluster ready",
		"cluster", name,
		"cloud", l.Cloud,
		"region", l.Region,
		"nodes", l.NumNodes)
	return cluster, nil
}

// installGpilet uploads the gpilet agent binary to every node and starts it.
// If no gpilet binary is found locally it silently skips deployment.
func (p *Provisioner) installGpilet(ctx context.Context, cluster *state.Cluster) error {
	local := gpiletBinaryPath(p.Dir)
	if local == "" {
		return nil
	}
	if err := p.waitAllSSH(ctx, cluster); err != nil {
		return err
	}
	var wg sync.WaitGroup
	errCh := make(chan error, len(cluster.Instances))
	for i := range cluster.Instances {
		node := &cluster.Instances[i]
		if node.PublicIP == "" {
			continue
		}
		wg.Add(1)
		go func(n *state.Node) {
			defer wg.Done()
			dst := "root@" + n.PublicIP + ":/usr/local/bin/gpilet"
			if err := p.copyFile(ctx, cluster.KeyPath, local, dst); err != nil {
				errCh <- fmt.Errorf("node %s: upload gpilet: %w", n.ID, err)
				return
			}
			script := `chmod +x /usr/local/bin/gpilet && mkdir -p /var/lib/gpilet && (pgrep -f gpilet >/dev/null 2>&1 || nohup /usr/local/bin/gpilet serve --dir /var/lib/gpilet --interval 10 > /var/log/gpilet.log 2>&1 &)`
			if code, err := p.runSSHStream(ctx, cluster.KeyPath, "root", n.PublicIP, script, nil); err != nil {
				errCh <- fmt.Errorf("node %s: start gpilet: %w", n.ID, err)
			} else if code != 0 {
				errCh <- fmt.Errorf("node %s: start gpilet failed (exit %d)", n.ID, code)
			}
		}(node)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		return err
	}
	return nil
}

func (p *Provisioner) copyFile(ctx context.Context, keyPath, src, dst string) error {
	scp := exec.CommandContext(ctx, "scp", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
		"-i", keyPath, src, dst)
	out, err := scp.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// gpiletBinaryPath returns the path to the gpilet agent binary, or "" if not
// available. Search order: GPI_HOME/bin/gpilet, next to the gpi executable,
// then GPI_GPILET.
func gpiletBinaryPath(dir string) string {
	candidates := []string{}
	if env := os.Getenv("GPI_GPILET"); env != "" {
		candidates = append(candidates, env)
	}
	if dir != "" {
		candidates = append(candidates, filepath.Join(dir, "bin", "gpilet"))
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "gpilet"))
	}
	for _, c := range candidates {
		if c != "" {
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				return c
			}
		}
	}
	return ""
}

// GpiletStatus returns the raw JSON status of a single node's gpilet agent.
func (p *Provisioner) GpiletStatus(ctx context.Context, cluster *state.Cluster, node *state.Node) (string, error) {
	script := `/usr/local/bin/gpilet status --dir /var/lib/gpilet 2>/dev/null || cat /var/lib/gpilet/status.json 2>/dev/null || echo '{"error":"gpilet not available"}'
`
	out, code, err := p.runSSHOutput(ctx, cluster.KeyPath, "root", node.PublicIP, script)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("gpilet status exit %d", code)
	}
	return out, nil
}

// GpiletHealth returns a human-readable one-line health summary for a node,
// e.g. "cpu 42%, load 2.50, mem 24.0/64.0GB, gpu 1, ray".
func (p *Provisioner) GpiletHealth(ctx context.Context, cluster *state.Cluster, node *state.Node) string {
	out, err := p.GpiletStatus(ctx, cluster, node)
	if err != nil {
		return fmt.Sprintf("unavailable: %v", err)
	}
	return formatGpiletHealth(out)
}

func formatGpiletHealth(raw string) string {
	var st struct {
		CPUs        int          `json:"cpus"`
		CPUUsagePct float64      `json:"cpu_usage_pct"`
		LoadAvg1    float64      `json:"load_avg_1"`
		MemTotalGB  float64      `json:"mem_total_gb"`
		MemUsedGB   float64      `json:"mem_used_gb"`
		GPUs        []gpilet.GPU `json:"gpus"`
		RayRunning  bool         `json:"ray_running"`
	}
	if err := json.Unmarshal([]byte(raw), &st); err != nil || st.CPUs == 0 {
		return fmt.Sprintf("gpilet offline (%s)", truncateString(raw, 40))
	}
	parts := []string{
		fmt.Sprintf("cpu %.0f%%", st.CPUUsagePct),
		fmt.Sprintf("load %.2f", st.LoadAvg1),
		fmt.Sprintf("mem %.1f/%.1fGB", st.MemUsedGB, st.MemTotalGB),
	}
	if len(st.GPUs) > 0 {
		parts = append(parts, fmt.Sprintf("gpu %d", len(st.GPUs)))
	}
	if st.RayRunning {
		parts = append(parts, "ray")
	}
	return strings.Join(parts, ", ")
}

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func (p *Provisioner) bootstrapRay(ctx context.Context, cluster *state.Cluster, ts *task.Task) error {
	head := cluster.Head()
	if head == nil {
		return errors.New("cluster has no head node")
	}
	if head.PrivateIP == "" {
		return errors.New("head node has no private IP")
	}
	keyPath := cluster.KeyPath

	labelsJSON := ""
	if ts != nil && ts.Resources != nil && len(ts.Resources.Labels) > 0 {
		b, err := json.Marshal(ts.Resources.Labels)
		if err != nil {
			return fmt.Errorf("marshal ray labels: %w", err)
		}
		labelsJSON = " --labels=" + shellQuote(string(b))
	}

	if err := p.waitAllSSH(ctx, cluster); err != nil {
		return err
	}

	if code, err := p.runSSHStream(ctx, keyPath, "root", head.PublicIP,
		rayInstallScript(), nil); err != nil {
		return err
	} else if code != 0 {
		return fmt.Errorf("install ray on head failed (exit %d)", code)
	}

	startCmd := fmt.Sprintf(`ray start --head --port=6379 --dashboard-port=8265 --disable-usage-stats --node-ip-address=%s%s`, head.PrivateIP, labelsJSON)
	if code, err := p.runSSHStream(ctx, keyPath, "root", head.PublicIP, startCmd, nil); err != nil {
		return err
	} else if code != 0 {
		return fmt.Errorf("ray start --head failed (exit %d)", code)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(cluster.Workers()))
	for _, worker := range cluster.Workers() {
		wg.Add(1)
		go func(n *state.Node) {
			defer wg.Done()
			if code, err := p.runSSHStream(ctx, keyPath, "root", n.PublicIP, rayInstallScript(), nil); err != nil {
				errCh <- fmt.Errorf("node %s: %w", n.ID, err)
				return
			} else if code != 0 {
				errCh <- fmt.Errorf("node %s: install ray failed (exit %d)", n.ID, code)
				return
			}
			joinCmd := fmt.Sprintf("ray start --address=%s:6379 --disable-usage-stats%s", head.PrivateIP, labelsJSON)
			if code, err := p.runSSHStream(ctx, keyPath, "root", n.PublicIP, joinCmd, nil); err != nil {
				errCh <- fmt.Errorf("node %s: %w", n.ID, err)
				return
			} else if code != 0 {
				errCh <- fmt.Errorf("node %s: ray start failed (exit %d)", n.ID, code)
			}
		}(worker)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		return err
	}
	return nil
}

func rayInstallScript() string {
	return `set -e
which pip3 >/dev/null 2>&1 || (sudo apt-get update -y >/dev/null 2>&1 && sudo apt-get install -y python3-pip >/dev/null 2>&1)
sudo pip3 install -q ray >/dev/null 2>&1 || true
ray --version >/dev/null 2>&1`
}

func (p *Provisioner) ensureKeyPair(ctx context.Context, prov cloud.Provider, name, cloudName, region string) (string, string, error) {
	keyName := fmt.Sprintf("gpi-%s-key", name)
	keyDir := filepath.Join(p.Dir, "keys")
	keyPath := filepath.Join(keyDir, keyName+".pem")
	if _, err := os.Stat(keyPath); err == nil {
		return keyName, keyPath, nil
	}
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		return "", "", err
	}
	privateKey, err := prov.CreateKeyPair(ctx, region, keyName)
	if err != nil {
		return "", "", fmt.Errorf("create keypair: %w", err)
	}
	if err := os.WriteFile(keyPath, []byte(privateKey), 0o600); err != nil {
		return "", "", err
	}
	return keyName, keyPath, nil
}

func (p *Provisioner) waitReady(ctx context.Context, prov cloud.Provider, cluster *state.Cluster) error {
	ids := make([]string, 0, len(cluster.Instances))
	for _, n := range cluster.Instances {
		ids = append(ids, n.ID)
	}
	timeout := 15 * time.Minute
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		instances, err := prov.DescribeInstances(ctx, cluster.Region, ids)
		if err != nil {
			return fmt.Errorf("describe instances: %w", err)
		}
		byID := map[string]*cloud.Instance{}
		for _, inst := range instances {
			byID[inst.ID] = inst
		}
		allRunning := true
		for i := range cluster.Instances {
			inst, ok := byID[cluster.Instances[i].ID]
			if !ok {
				allRunning = false
				continue
			}
			cluster.Instances[i].PublicIP = inst.PublicIP
			cluster.Instances[i].PrivateIP = inst.PrivateIP
			cluster.Instances[i].Status = string(inst.Status)
			if inst.Status != cloud.StatusRunning {
				allRunning = false
			}
		}
		p.Store.UpdateCluster(cluster.Name, func(c *state.Cluster) error {
			c.Instances = cluster.Instances
			c.Status = state.ClusterProvisioning
			return nil
		})
		if allRunning {
			return p.Store.UpdateCluster(cluster.Name, func(c *state.Cluster) error {
				c.Instances = cluster.Instances
				c.Status = state.ClusterUp
				return nil
			})
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for cluster %s to become ready", cluster.Name)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// RunTask runs a task against an existing cluster: waits for SSH on all
// nodes, optionally uploads the workdir to head, runs setup on all nodes
// (parallel for multi-node) and the run command on the head node.
func (p *Provisioner) RunTask(ctx context.Context, name string, ts *task.Task, stream func(line string)) (int, error) {
	logger.Info("running task", "cluster", name, "task", ts.Name)
	cluster, err := p.Store.GetCluster(name)
	if err != nil {
		return -1, err
	}
	head := cluster.Head()
	if head == nil {
		return -1, errors.New("cluster has no head node")
	}
	if head.PublicIP == "" {
		return -1, fmt.Errorf("cluster %s has no head public IP", name)
	}
	if err := p.waitAllSSH(ctx, cluster); err != nil {
		return -1, err
	}
	if ts.Workdir != "" {
		if err := p.copyDir(ctx, cluster.KeyPath, ts.Workdir, "root@"+head.PublicIP+":/root/gpi-workdir"); err != nil {
			return -1, fmt.Errorf("upload workdir: %w", err)
		}
	}
	if ts.Setup != "" {
		if cluster.NumNodes > 1 {
			if code, err := p.runOnAllNodes(ctx, cluster, ts.Setup, stream); err != nil {
				return code, err
			}
		} else {
			if code, err := p.runSSHStream(ctx, cluster.KeyPath, "root", head.PublicIP, ts.Setup, stream); err != nil {
				return code, err
			}
		}
	}
	if ts.Run != "" {
		if code, err := p.runSSHStream(ctx, cluster.KeyPath, "root", head.PublicIP, ts.Run, stream); err != nil {
			return code, err
		}
	}
	return 0, nil
}

func (p *Provisioner) runOnAllNodes(ctx context.Context, cluster *state.Cluster, script string, stream func(string)) (int, error) {
	var wg sync.WaitGroup
	errCh := make(chan error, len(cluster.Instances))
	codeCh := make(chan int, len(cluster.Instances))
	for _, node := range cluster.Instances {
		wg.Add(1)
		go func(n state.Node) {
			defer wg.Done()
			code, err := p.runSSHStream(ctx, cluster.KeyPath, "root", n.PublicIP, script, stream)
			if err != nil {
				errCh <- fmt.Errorf("node %s: %w", n.ID, err)
				return
			}
			codeCh <- code
		}(node)
	}
	wg.Wait()
	close(errCh)
	close(codeCh)
	for err := range errCh {
		return -1, err
	}
	for code := range codeCh {
		if code != 0 {
			return code, nil
		}
	}
	return 0, nil
}

func (p *Provisioner) waitAllSSH(ctx context.Context, cluster *state.Cluster) error {
	for i := range cluster.Instances {
		node := &cluster.Instances[i]
		if node.PublicIP == "" {
			continue
		}
		if err := p.waitSSH(ctx, cluster.KeyPath, "root", node.PublicIP); err != nil {
			return err
		}
	}
	return nil
}

// Exec runs an arbitrary command on the cluster's head node.
func (p *Provisioner) Exec(ctx context.Context, name, cmd string, stream func(line string)) (int, error) {
	cluster, err := p.Store.GetCluster(name)
	if err != nil {
		return -1, err
	}
	ip := cluster.GetNodeIP()
	if ip == "" {
		return -1, fmt.Errorf("cluster %s has no public IP", name)
	}
	if err := p.waitSSH(ctx, cluster.KeyPath, "root", ip); err != nil {
		return -1, err
	}
	return p.runSSHStream(ctx, cluster.KeyPath, "root", ip, cmd, stream)
}

// Down terminates all instances of a cluster and removes its state record.
func (p *Provisioner) Down(ctx context.Context, name string) error {
	logger.Info("terminating cluster", "cluster", name)
	cluster, err := p.Store.GetCluster(name)
	if err != nil {
		return err
	}
	prov, err := p.providerForCluster(cluster)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(cluster.Instances))
	for _, n := range cluster.Instances {
		ids = append(ids, n.ID)
	}
	if len(ids) > 0 {
		if err := prov.TerminateInstances(ctx, cluster.Region, ids); err != nil {
			return err
		}
	}
	p.recordClusterEvent(name, cluster.Status, state.ClusterDown, state.EventDown, "cluster terminated")
	return p.Store.DeleteCluster(name)
}

// Stop powers off a cluster's instances (billing stops, data retained).
func (p *Provisioner) Stop(ctx context.Context, name string) error {
	logger.Info("stopping cluster", "cluster", name)
	cluster, err := p.Store.GetCluster(name)
	if err != nil {
		return err
	}
	prov, err := p.providerForCluster(cluster)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(cluster.Instances))
	for _, n := range cluster.Instances {
		ids = append(ids, n.ID)
	}
	if err := prov.StopInstances(ctx, cluster.Region, ids); err != nil {
		return err
	}
	p.recordClusterEvent(name, cluster.Status, state.ClusterStopped, state.EventStop, "cluster stopped")
	return p.Store.UpdateCluster(name, func(c *state.Cluster) error {
		c.Status = state.ClusterStopped
		return nil
	})
}

// Start powers a stopped cluster back on and waits for it to be ready.
func (p *Provisioner) Start(ctx context.Context, name string) error {
	logger.Info("starting cluster", "cluster", name)
	cluster, err := p.Store.GetCluster(name)
	if err != nil {
		return err
	}
	prov, err := p.providerForCluster(cluster)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(cluster.Instances))
	for _, n := range cluster.Instances {
		ids = append(ids, n.ID)
	}
	if err := prov.StartInstances(ctx, cluster.Region, ids); err != nil {
		return err
	}
	p.recordClusterEvent(name, cluster.Status, state.ClusterProvisioning, state.EventStart, "cluster starting")
	if err := p.Store.UpdateCluster(name, func(c *state.Cluster) error {
		c.Status = state.ClusterProvisioning
		return nil
	}); err != nil {
		return err
	}
	return p.waitReady(ctx, prov, cluster)
}

func (p *Provisioner) waitSSH(ctx context.Context, keyPath, user, ip string) error {
	deadline := time.Now().Add(10 * time.Minute)
	for {
		cmd := exec.CommandContext(ctx, sshBin(), "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null", "-i", keyPath, user+"@"+ip, "true")
		if cmd.Run() == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for SSH on %s", ip)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func (p *Provisioner) runSSHOutput(ctx context.Context, keyPath, user, ip, script string) (string, int, error) {
	cmd := exec.CommandContext(ctx, sshBin(), "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
		"-i", keyPath, user+"@"+ip, "bash -s")
	cmd.Stdin = strings.NewReader(script)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			return "", -1, err
		}
	}
	return strings.TrimSpace(out.String()), code, nil
}

func (p *Provisioner) runSSHStream(ctx context.Context, keyPath, user, ip, script string, stream func(string)) (int, error) {
	cmd := exec.CommandContext(ctx, sshBin(), "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
		"-i", keyPath, user+"@"+ip, "bash -s")
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = streamWriter(stream)
	cmd.Stderr = streamWriter(stream)
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			return -1, err
		}
	}
	return code, nil
}

func (p *Provisioner) copyDir(ctx context.Context, keyPath, src, dst string) error {
	scp := exec.CommandContext(ctx, "scp", "-r", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
		"-i", keyPath, src, dst)
	out, err := scp.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

func streamWriter(stream func(string)) *lineWriter {
	return &lineWriter{stream: stream}
}

func sshBin() string {
	if bin := os.Getenv("GPI_SSH"); bin != "" {
		return bin
	}
	return "ssh"
}

func cloneMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func toCloudCreds(cc *task.CloudCredentials) *cloud.Credentials {
	if cc == nil {
		return nil
	}
	return &cloud.Credentials{
		AccessKeyID:     cc.AccessKeyID,
		SecretAccessKey: cc.SecretAccessKey,
		Region:          cc.Region,
	}
}

func stateCloudCreds(cc *task.CloudCredentials) *state.CloudCreds {
	if cc == nil {
		return nil
	}
	return &state.CloudCreds{
		AccessKeyID:     cc.AccessKeyID,
		SecretAccessKey: cc.SecretAccessKey,
		Region:          cc.Region,
	}
}

func (p *Provisioner) providerForCluster(cluster *state.Cluster) (cloud.Provider, error) {
	var creds *cloud.Credentials
	if cluster.CloudCreds != nil {
		creds = &cloud.Credentials{
			AccessKeyID:     cluster.CloudCreds.AccessKeyID,
			SecretAccessKey: cluster.CloudCreds.SecretAccessKey,
			Region:          cluster.CloudCreds.Region,
		}
	}
	prov, err := cloud.New(cluster.Cloud, creds)
	if err != nil {
		return nil, err
	}
	if prov == nil {
		return nil, fmt.Errorf("cloud provider %q not registered", cluster.Cloud)
	}
	return prov, nil
}

// recordClusterEvent appends a lifecycle event for a cluster.
func (p *Provisioner) recordClusterEvent(name string, from, to state.ClusterStatus, typ state.ClusterEventType, reason string) {
	_ = p.Store.AddClusterEvent(&state.ClusterEvent{
		ClusterName:    name,
		StartingStatus: string(from),
		EndingStatus:   string(to),
		Reason:         reason,
		Type:           typ,
	})
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func bootstrapScript(clusterName string) string {
	return fmt.Sprintf(`#!/bin/bash
set -e
echo "gpi bootstrap started for %s" >> /var/log/gpi-bootstrap.log
grep -q gpi-bootstrap /etc/rc.local 2>/dev/null || true
`, clusterName)
}

func taskYAML(ts *task.Task) string {
	return ts.String()
}
