package gcp

import (
	"testing"

	"github.com/acmestack/gpi/internal/cloud"
)

// ── provider.go ──────────────────────────────────────────────────────────────

func TestCloudName(t *testing.T) {
	if CloudName != "gcp" {
		t.Fatalf("CloudName = %q, want %q", CloudName, "gcp")
	}
}

func TestProviderName(t *testing.T) {
	var p Provider
	if p.Name() != "gcp" {
		t.Fatalf("Provider.Name() = %q, want %q", p.Name(), "gcp")
	}
}

func TestNewProvider(t *testing.T) {
	creds := &Credentials{ProjectID: "test-project"}
	p := NewProvider(creds)
	if p.Name() != "gcp" {
		t.Fatalf("NewProvider().Name() = %q, want %q", p.Name(), "gcp")
	}
}

func TestNewProvider_NilCreds(t *testing.T) {
	p := NewProvider(nil)
	if p.Name() != "gcp" {
		t.Fatalf("NewProvider(nil).Name() = %q, want %q", p.Name(), "gcp")
	}
}

func TestDefaultRegion(t *testing.T) {
	tests := []struct {
		name  string
		creds *Credentials
		want  string
	}{
		{"with project", &Credentials{ProjectID: "proj"}, "us-central1"},
		{"nil creds", nil, ""},
		{"empty project", &Credentials{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProvider(tt.creds)
			got := p.defaultRegion()
			if got != tt.want {
				t.Errorf("defaultRegion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProjectID(t *testing.T) {
	tests := []struct {
		name    string
		creds   *Credentials
		want    string
		wantErr bool
	}{
		{"with project", &Credentials{ProjectID: "my-proj"}, "my-proj", false},
		{"nil creds", nil, "", true},
		{"empty project", &Credentials{}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProvider(tt.creds)
			got, err := p.projectID()
			if (err != nil) != tt.wantErr {
				t.Errorf("projectID() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("projectID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetImage(t *testing.T) {
	p := NewProvider(nil)
	img, err := p.GetImage(nil, "", "")
	if err != nil {
		t.Fatalf("GetImage() error = %v", err)
	}
	if img == "" {
		t.Error("GetImage() returned empty string")
	}
}

func TestDescribeZones(t *testing.T) {
	p := NewProvider(nil)
	zones, err := p.DescribeZones(nil, "us-central1")
	if err != nil {
		t.Fatalf("DescribeZones() error = %v", err)
	}
	if len(zones) != 3 {
		t.Fatalf("DescribeZones() returned %d zones, want 3", len(zones))
	}
	for _, z := range zones {
		if z == "" {
			t.Error("DescribeZones() returned empty zone")
		}
	}
}

func TestCreateKeyPair(t *testing.T) {
	p := NewProvider(nil)
	key, err := p.CreateKeyPair(nil, "", "")
	if err != nil {
		t.Fatalf("CreateKeyPair() error = %v", err)
	}
	if key != "" {
		t.Errorf("CreateKeyPair() = %q, want empty", key)
	}
}

func TestDeleteKeyPair(t *testing.T) {
	p := NewProvider(nil)
	if err := p.DeleteKeyPair(nil, "", ""); err != nil {
		t.Fatalf("DeleteKeyPair() error = %v", err)
	}
}

// ── client.go ────────────────────────────────────────────────────────────────

func TestTruncate(t *testing.T) {
	tests := []struct {
		s    string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
		{"ab", 1, "a..."},
		{"abc", 3, "abc"},
	}
	for _, tt := range tests {
		got := truncate(tt.s, tt.n)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
		}
	}
}

func TestHomeDir(t *testing.T) {
	// homeDir should not panic and may return empty in CI
	_ = homeDir()
}

func TestAPIError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  APIError
		want string
	}{
		{
			"with errors",
			APIError{Code: 404, Errors: []struct {
				Code    string
				Message string
			}{{Code: "notFound", Message: "resource not found"}}},
			"gcp api: resource not found (notFound)",
		},
		{
			"without errors",
			APIError{Code: 500, Message: "internal error"},
			"gcp api: internal error (http 500)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.want {
				t.Errorf("APIError.Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewClient_NoProjectID(t *testing.T) {
	t.Setenv("GCP_PROJECT_ID", "")
	_, err := NewClient("us-central1")
	if err == nil {
		t.Fatal("NewClient() without GCP_PROJECT_ID should fail")
	}
}

// ── metadata.go ──────────────────────────────────────────────────────────────

func TestMetadataCloud(t *testing.T) {
	var p Provider
	if p.Cloud() != "gcp" {
		t.Errorf("Cloud() = %q, want %q", p.Cloud(), "gcp")
	}
}

func TestMetadataSpecsTTL(t *testing.T) {
	var p Provider
	ttl := p.SpecsTTL()
	if ttl <= 0 {
		t.Errorf("SpecsTTL() = %v, want > 0", ttl)
	}
}

func TestMetadataPriceTTL(t *testing.T) {
	var p Provider
	ttl := p.PriceTTL()
	if ttl <= 0 {
		t.Errorf("PriceTTL() = %v, want > 0", ttl)
	}
}

func TestExtractGPUType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"nvidia-tesla-t4", "T4"},
		{"nvidia-tesla-v100", "V100"},
		{"nvidia-tesla-a100", "A100"},
		{"nvidia-tesla-k80", "K80"},
		{"nvidia-tesla-p100", "P100"},
		{"nvidia-tesla-p4", "P4"},
		{"nvidia-l4", "L4"},
		{"nvidia-a100-80gb", "A100-80GB"},
		{"nvidia-h100-80gb", "H100-80GB"},
		{"amd-instinct-mi250", "AMD-INSTINCT-MI250"},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractGPUType(tt.input)
		if got != tt.want {
			t.Errorf("extractGPUType(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExtractMachineType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"projects/my-project/zones/us-central1-a/machineTypes/n2-standard-4", "n2-standard-4"},
		{"n2-standard-4", "n2-standard-4"},
		{"projects/proj/zones/zone/machineTypes/e2-medium", "e2-medium"},
		{"projects/p/zones/z/machineTypes/a2-highgpu-8g", "a2-highgpu-8g"},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractMachineType(tt.input)
		if got != tt.want {
			t.Errorf("extractMachineType(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseZone(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"projects/proj/zones/us-central1-a", "us-central1-a"},
		{"us-central1-a", "us-central1-a"},
		{"projects/p/zones/europe-west1-b", "europe-west1-b"},
		{"", ""},
	}
	for _, tt := range tests {
		got := ParseZone(tt.input)
		if got != tt.want {
			t.Errorf("ParseZone(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseMachineType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"projects/p/zones/z/machineTypes/n2-standard-4", "n2-standard-4"},
		{"e2-medium", "e2-medium"},
		{"", ""},
	}
	for _, tt := range tests {
		got := ParseMachineType(tt.input)
		if got != tt.want {
			t.Errorf("ParseMachineType(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ── status conversion ────────────────────────────────────────────────────────

func TestGcpStatusToCloud(t *testing.T) {
	tests := []struct {
		input string
		want  cloud.InstanceStatus
	}{
		{"RUNNING", cloud.StatusRunning},
		{"running", cloud.StatusRunning},
		{"STOPPED", cloud.StatusStopped},
		{"stopped", cloud.StatusStopped},
		{"PROVISIONING", cloud.StatusPending},
		{"STAGING", cloud.StatusPending},
		{"TERMINATED", cloud.StatusTerminated},
		{"TERMINATING", cloud.StatusTerminated},
		{"SUSPENDED", cloud.StatusStopped},
		{"REPAIRING", cloud.StatusPending},
		{"UNKNOWN", cloud.StatusUnknown},
		{"", cloud.StatusUnknown},
		{"RANDOM_STATUS", cloud.StatusUnknown},
	}
	for _, tt := range tests {
		got := gcpStatusToCloud(tt.input)
		if got != tt.want {
			t.Errorf("gcpStatusToCloud(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// ── provider interface compliance ────────────────────────────────────────────

func TestProviderImplementsInterface(t *testing.T) {
	var p Provider
	var _ cloud.Provider = p
}

func TestProviderCatalogSource(t *testing.T) {
	var p Provider
	// Verify Provider has the catalog.Source methods
	_ = p.Cloud()
	_ = p.SpecsTTL()
	_ = p.PriceTTL()
}
