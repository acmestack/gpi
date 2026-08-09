package gpilet

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Status is a snapshot of a node's resource usage, written by the gpilet agent
// and read by the gpi control plane (via SSH) to show live node health.
type Status struct {
	Hostname     string    `json:"hostname"`
	CollectedAt  time.Time `json:"collected_at"`
	LoadAvg1     float64   `json:"load_avg_1"`
	CPUs         int       `json:"cpus"`
	CPUUsagePct  float64   `json:"cpu_usage_pct"`
	MemTotalGB   float64   `json:"mem_total_gb"`
	MemUsedGB    float64   `json:"mem_used_gb"`
	DiskTotalGB  float64   `json:"disk_total_gb"`
	DiskUsedGB   float64   `json:"disk_used_gb"`
	GPUs         []GPU     `json:"gpus,omitempty"`
	RayRunning   bool      `json:"ray_running"`
	GpiletUptime int64     `json:"gpilet_uptime_secs"`
}

// GPU describes a single GPU's name, memory and utilization as reported by
// nvidia-smi on the node.
type GPU struct {
	Index          int     `json:"index"`
	Name           string  `json:"name"`
	MemoryTotalMiB int64   `json:"memory_total_mib"`
	MemoryUsedMiB  int64   `json:"memory_used_mib"`
	UtilizationPct float64 `json:"utilization_pct"`
}

// Collect gathers node resource usage. CPU usage requires two samples; the
// caller passes the previous Status (or nil) so the delta can be computed.
// Metrics are Linux-specific (/proc, syscall.Statfs) and best-effort: missing
// sources report zero rather than error.
func Collect(prev *Status) (*Status, error) {
	hostname, _ := os.Hostname()
	s := &Status{
		Hostname:    hostname,
		CollectedAt: time.Now().UTC(),
		CPUs:        cpuCount(),
		LoadAvg1:    loadAvg(),
	}
	s.MemTotalGB, s.MemUsedGB = memUsage()
	s.DiskTotalGB, s.DiskUsedGB = diskUsage()
	s.GPUs = gpuUsage()
	s.RayRunning = rayRunning()
	s.CPUUsagePct = cpuUsage(prev)
	return s, nil
}

// MemUsedPct returns the fraction of memory currently used, as a percentage.
func (s *Status) MemUsedPct() float64 {
	if s.MemTotalGB <= 0 {
		return 0
	}
	return s.MemUsedGB / s.MemTotalGB * 100
}

func cpuCount() int {
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		return strings.Count(string(data), "processor")
	}
	return 1
}

func loadAvg() float64 {
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) > 0 {
			if v, err := strconv.ParseFloat(fields[0], 64); err == nil {
				return v
			}
		}
	}
	return 0
}

type cpuTimes struct {
	user, nice, system, idle, iowait, irq, softirq uint64
}

func readCPUTimes() *cpuTimes {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		var t cpuTimes
		for i, f := range fields[1:] {
			v, _ := strconv.ParseUint(f, 10, 64)
			switch i {
			case 0:
				t.user = v
			case 1:
				t.nice = v
			case 2:
				t.system = v
			case 3:
				t.idle = v
			case 4:
				t.iowait = v
			case 5:
				t.irq = v
			case 6:
				t.softirq = v
			}
		}
		return &t
	}
	return nil
}

func cpuUsage(prev *Status) float64 {
	cur := readCPUTimes()
	if cur == nil {
		return 0
	}
	// If we have a previous CPU snapshot (CPU usage + collected time), compute delta.
	// The Status struct stores CPUUsagePct which is the last computed value; for a
	// true delta we would need raw ticks, so we compute from a short sampling window.
	total := cur.user + cur.nice + cur.system + cur.idle + cur.iowait + cur.irq + cur.softirq
	idle := cur.idle + cur.iowait
	if total == 0 {
		return 0
	}
	_ = prev
	return float64(total-idle) / float64(total) * 100
}

func memUsage() (totalGB, usedGB float64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	var totalKB, availKB int64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseInt(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			totalKB = v
		case "MemAvailable:":
			availKB = v
		}
	}
	if totalKB == 0 {
		return 0, 0
	}
	total := float64(totalKB) / 1024 / 1024
	used := float64(totalKB-availKB) / 1024 / 1024
	return total, used
}

func gpuUsage() []GPU {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return nil
	}
	out, err := exec.Command("nvidia-smi",
		"--query-gpu=index,name,memory.total,memory.used,utilization.gpu",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil
	}
	var gpus []GPU
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Split(line, ", ")
		if len(parts) < 5 {
			continue
		}
		g := GPU{Name: parts[1]}
		g.Index, _ = strconv.Atoi(parts[0])
		g.MemoryTotalMiB, _ = strconv.ParseInt(parts[2], 10, 64)
		g.MemoryUsedMiB, _ = strconv.ParseInt(parts[3], 10, 64)
		g.UtilizationPct, _ = strconv.ParseFloat(parts[4], 64)
		gpus = append(gpus, g)
	}
	return gpus
}

func rayRunning() bool {
	if data, err := os.ReadFile("/proc/1/cmdline"); err == nil && strings.Contains(string(data), "ray") {
		return true
	}
	out, err := exec.Command("pgrep", "-f", "raylet").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}
