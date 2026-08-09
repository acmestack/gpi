package cloud

import (
	"context"
	"testing"
	"time"

	"github.com/acmestack/gpi/internal/cloud/catalog"
)

// combinedProvider implements both cloud.Provider and catalog.Source in one
// struct, mirroring how real clouds register today.
type combinedProvider struct{}

func (combinedProvider) Name() string { return "testcloud" }
func (combinedProvider) Cloud() string {
	return "testcloud"
}
func (combinedProvider) SpecsTTL() time.Duration { return time.Hour }
func (combinedProvider) PriceTTL() time.Duration { return time.Minute }

func (combinedProvider) Regions(context.Context) ([]string, error) { return []string{"r1"}, nil }
func (combinedProvider) FetchSpecs(context.Context, string) ([]*catalog.Instance, error) {
	return []*catalog.Instance{{Cloud: "testcloud", Region: "r1", InstanceType: "t", VCPUs: 2}}, nil
}
func (combinedProvider) FetchPrices(context.Context, string, []string) (map[string]catalog.Price, error) {
	return nil, nil
}

// Provider lifecycle methods: minimal stubs to satisfy cloud.Provider.
func (combinedProvider) RunInstances(context.Context, *LaunchSpec) ([]*Instance, error) {
	return nil, nil
}
func (combinedProvider) ListInstances(context.Context, string, string) ([]*Instance, error) {
	return nil, nil
}
func (combinedProvider) DescribeInstances(context.Context, string, []string) ([]*Instance, error) {
	return nil, nil
}
func (combinedProvider) StopInstances(context.Context, string, []string) error  { return nil }
func (combinedProvider) StartInstances(context.Context, string, []string) error { return nil }
func (combinedProvider) TerminateInstances(context.Context, string, []string) error {
	return nil
}
func (combinedProvider) GetPublicIP(context.Context, string, string) (string, error) { return "", nil }
func (combinedProvider) DescribeZones(context.Context, string) ([]string, error)     { return nil, nil }
func (combinedProvider) CreateKeyPair(context.Context, string, string) (string, error) {
	return "", nil
}
func (combinedProvider) DeleteKeyPair(context.Context, string, string) error { return nil }
func (combinedProvider) CreateSecurityGroup(context.Context, string, string, string, string) (string, error) {
	return "", nil
}
func (combinedProvider) AuthorizeSecurityGroup(context.Context, string, string, int, int, string) error {
	return nil
}
func (combinedProvider) CreateVPC(context.Context, string, string, string) (string, error) {
	return "", nil
}
func (combinedProvider) CreateVSwitch(context.Context, string, string, string, string, string) (string, error) {
	return "", nil
}
func (combinedProvider) ListVSwitches(context.Context, string, string) ([]VSwitch, error) {
	return nil, nil
}
func (combinedProvider) GetImage(context.Context, string, string) (string, error) { return "", nil }

func TestRegisterAutoRegistersMetadataSource(t *testing.T) {
	Register(combinedProvider{})
	defer func() {
		delete(registry, "testcloud")
		catalog.ResetForTest()
	}()

	if !Has("testcloud") {
		t.Fatal("provider not registered")
	}
	if !catalog.HasCloud("testcloud") {
		t.Fatal("metadata source not auto-registered from a combined provider")
	}
	insts, err := catalog.SourceFor("testcloud").FetchSpecs(context.Background(), "r1")
	if err != nil || len(insts) == 0 || insts[0].InstanceType != "t" {
		t.Fatalf("metadata source not functional: %v %v", insts, err)
	}
}
