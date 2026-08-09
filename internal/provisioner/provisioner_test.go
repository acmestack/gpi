package provisioner

import "testing"

func TestFormatGpiletHealth(t *testing.T) {
	raw := `{
  "hostname": "node-1",
  "collected_at": "2026-08-12T06:00:00Z",
  "load_avg_1": 2.5,
  "cpus": 16,
  "cpu_usage_pct": 42.5,
  "mem_total_gb": 64,
  "mem_used_gb": 24,
  "disk_total_gb": 500,
  "disk_used_gb": 100,
  "gpus": [{"index":0,"name":"A100","memory_total_mib":40960,"memory_used_mib":10240,"utilization_pct":80}],
  "ray_running": true,
  "gpilet_uptime_secs": 100
}`
	got := formatGpiletHealth(raw)
	want := "cpu 42%, load 2.50, mem 24.0/64.0GB, gpu 1, ray"
	if got != want {
		t.Fatalf("formatGpiletHealth = %q, want %q", got, want)
	}
}

func TestFormatGpiletHealthOffline(t *testing.T) {
	if got := formatGpiletHealth("garbage"); got == "garbage" {
		t.Fatalf("expected formatted offline message, got %q", got)
	}
	if got := formatGpiletHealth(""); got == "" {
		t.Fatal("empty input should produce offline message")
	}
}
