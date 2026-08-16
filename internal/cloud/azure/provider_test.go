package azure

import (
	"testing"

	"github.com/acmestack/gpi/internal/cloud"
)

// ── provider.go ──────────────────────────────────────────────────────────────

func TestCloudName(t *testing.T) {
	if CloudName != "azure" {
		t.Fatalf("CloudName = %q, want %q", CloudName, "azure")
	}
}

func TestProviderName(t *testing.T) {
	var p Provider
	if p.Name() != "azure" {
		t.Fatalf("Provider.Name() = %q, want %q", p.Name(), "azure")
	}
}

func TestNewProvider(t *testing.T) {
	creds := &Credentials{
		SubscriptionID: "test-sub",
		TenantID:       "test-tenant",
		ClientID:       "test-client",
		ClientSecret:   "test-secret",
	}
	p := NewProvider(creds)
	if p.Name() != "azure" {
		t.Fatalf("NewProvider().Name() = %q, want %q", p.Name(), "azure")
	}
}

func TestNewProvider_NilCreds(t *testing.T) {
	p := NewProvider(nil)
	if p.Name() != "azure" {
		t.Fatalf("NewProvider(nil).Name() = %q, want %q", p.Name(), "azure")
	}
}

func TestSubscriptionPath(t *testing.T) {
	tests := []struct {
		name  string
		creds *Credentials
		want  string
	}{
		{"with sub", &Credentials{SubscriptionID: "sub-123"}, "/subscriptions/sub-123"},
		{"nil creds", nil, "/subscriptions/"},
		{"empty sub", &Credentials{}, "/subscriptions/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProvider(tt.creds)
			got := p.subscriptionPath()
			if got != tt.want {
				t.Errorf("subscriptionPath() = %q, want %q", got, tt.want)
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
	zones, err := p.DescribeZones(nil, "eastus")
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

func TestRegions(t *testing.T) {
	p := NewProvider(nil)
	regions, err := p.Regions(nil)
	if err != nil {
		t.Fatalf("Regions() error = %v", err)
	}
	if len(regions) == 0 {
		t.Fatal("Regions() returned empty list")
	}
	for _, r := range regions {
		if r == "" {
			t.Error("Regions() returned empty region")
		}
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
	_ = homeDir()
}

func TestAPIError_Error(t *testing.T) {
	err := APIError{Code: "InvalidSubscriptionId", Message: "The subscription is invalid"}
	got := err.Error()
	want := "azure api: The subscription is invalid (InvalidSubscriptionId)"
	if got != want {
		t.Errorf("APIError.Error() = %q, want %q", got, want)
	}
}

func TestNewClient_NoSubscriptionID(t *testing.T) {
	t.Setenv("AZURE_SUBSCRIPTION_ID", "")
	_, err := NewClient("eastus")
	if err == nil {
		t.Fatal("NewClient() without AZURE_SUBSCRIPTION_ID should fail")
	}
}

func TestNewClient_NoTenantID(t *testing.T) {
	t.Setenv("AZURE_SUBSCRIPTION_ID", "test-sub")
	t.Setenv("AZURE_TENANT_ID", "")
	t.Setenv("AZURE_CLIENT_ID", "")
	t.Setenv("AZURE_CLIENT_SECRET", "")
	_, err := NewClient("eastus")
	if err == nil {
		t.Fatal("NewClient() without tenant/client credentials should fail")
	}
}

// ── metadata.go ──────────────────────────────────────────────────────────────

func TestMetadataCloud(t *testing.T) {
	var p Provider
	if p.Cloud() != "azure" {
		t.Errorf("Cloud() = %q, want %q", p.Cloud(), "azure")
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

// ── status conversion ────────────────────────────────────────────────────────

func TestAzureStatusToCloud(t *testing.T) {
	tests := []struct {
		input string
		want  cloud.InstanceStatus
	}{
		{"Succeeded", cloud.StatusRunning},
		{"succeeded", cloud.StatusRunning},
		{"running", cloud.StatusRunning},
		{"RUNNING", cloud.StatusRunning},
		{"stopped", cloud.StatusStopped},
		{"STOPPED", cloud.StatusStopped},
		{"deallocating", cloud.StatusStopping},
		{"deallocating", cloud.StatusStopping},
		{"creating", cloud.StatusPending},
		{"updating", cloud.StatusPending},
		{"failed", cloud.StatusTerminated},
		{"FAILED", cloud.StatusTerminated},
		{"unknown", cloud.StatusUnknown},
		{"UNKNOWN", cloud.StatusUnknown},
		{"", cloud.StatusUnknown},
		{"random_status", cloud.StatusUnknown},
		{"provisioning", cloud.StatusPending},
		{"stopping", cloud.StatusStopping},
	}
	for _, tt := range tests {
		got := azureStatusToCloud(tt.input)
		if got != tt.want {
			t.Errorf("azureStatusToCloud(%q) = %v, want %v", tt.input, got, tt.want)
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
