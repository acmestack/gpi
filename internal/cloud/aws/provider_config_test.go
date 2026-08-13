package aws

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/acmestack/gpi/internal/config"
)

func mkTag(name string) struct {
	Item []struct {
		Key   string `xml:"key"`
		Value string `xml:"value"`
	} `xml:"item"`
} {
	return struct {
		Item []struct {
			Key   string `xml:"key"`
			Value string `xml:"value"`
		} `xml:"item"`
	}{Item: []struct {
		Key   string `xml:"key"`
		Value string `xml:"value"`
	}{{Key: "Name", Value: name}}}
}

func TestMatchVPCByName(t *testing.T) {
	vpcs := []vpcItem{
		{VpcId: "vpc-1", IsDefault: true},
		{VpcId: "vpc-2", TagSet: mkTag("prod")},
	}
	cases := []struct {
		want []string
		id   string
	}{
		{[]string{"prod"}, "vpc-2"},
		{[]string{"missing", "vpc-1"}, "vpc-1"},
		{[]string{"missing"}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := matchVPCByName(vpcs, c.want); got != c.id {
			t.Fatalf("matchVPCByName(%v) = %q, want %q", c.want, got, c.id)
		}
	}
}

func TestMatchSubnetsByName(t *testing.T) {
	subs := []subnetItem{
		{SubnetId: "subnet-1", AvailabilityZone: "us-east-1a"},
		{SubnetId: "subnet-2", TagSet: mkTag("ml"), AvailabilityZone: "us-east-1b"},
	}
	got := matchSubnetsByName(subs, []string{"ml", "subnet-1"})
	if len(got) != 2 || got[0].SubnetId != "subnet-2" || got[1].SubnetId != "subnet-1" {
		t.Fatalf("matchSubnetsByName = %+v", got)
	}
	if len(matchSubnetsByName(subs, []string{"nope"})) != 0 {
		t.Fatal("expected no match")
	}
}

func TestMatchSecurityGroupByID(t *testing.T) {
	groups := []securityGroupItem{
		{GroupId: "sg-1", GroupName: "dev"},
		{GroupId: "sg-2", GroupName: "prod"},
	}
	if got := matchSecurityGroupByID(groups, "prod"); got != "sg-2" {
		t.Fatalf("match by name = %q", got)
	}
	if got := matchSecurityGroupByID(groups, "sg-1"); got != "sg-1" {
		t.Fatalf("match by id = %q", got)
	}
	if got := matchSecurityGroupByID(groups, "missing"); got != "" {
		t.Fatalf("expected no match, got %q", got)
	}
}

// TestProviderReadsConfig verifies the provider's network helpers consult the
// user config for VPC reuse (integration with the config package).
func TestProviderReadsConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, config.FileName)
	content := "aws:\n  vpc_names: [prod]\n  subnet_names: [ml]\n  security_group_name: sg-prod\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	config.SetPath(cfgPath)
	defer config.Reset()

	vpcs := []vpcItem{
		{VpcId: "vpc-default", IsDefault: true},
		{VpcId: "vpc-prod", TagSet: mkTag("prod")},
	}
	got := matchVPCByName(vpcs, LoadConfig().VPCNames)
	if got != "vpc-prod" {
		t.Fatalf("provider should pick configured vpc, got %q", got)
	}
}
