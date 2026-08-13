package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTmp writes a file under a temp dir and returns the path.
func writeTmp(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// awsSec decodes the "aws" section for assertions.
func awsSec(t *testing.T, c *Config) *struct {
	VPCNames          []string `yaml:"vpc_names"`
	SecurityGroupName string   `yaml:"security_group_name"`
	SubnetNames       []string `yaml:"subnet_names"`
} {
	t.Helper()
	out := &struct {
		VPCNames          []string `yaml:"vpc_names"`
		SecurityGroupName string   `yaml:"security_group_name"`
		SubnetNames       []string `yaml:"subnet_names"`
	}{}
	if err := c.Section("aws", out); err != nil {
		t.Fatalf("Section(aws): %v", err)
	}
	return out
}

func TestLoadUserOnly(t *testing.T) {
	dir := t.TempDir()
	writeTmp(t, dir, FileName, `
aws:
  vpc_names: [vpc-aaa]
  security_group_name: my-sg
aliyun:
  vpc_id: vpc-bbb
  vswitch_ids: [vsw-1, vsw-2]
  security_group_id: sg-ccc
allowed_clouds: [aws]
region: cn-hangzhou
use_spot: true
`)
	SetPath(filepath.Join(dir, FileName))
	defer Reset()

	c := Load()
	aws := awsSec(t, c)
	if len(aws.VPCNames) != 1 || aws.VPCNames[0] != "vpc-aaa" {
		t.Fatalf("bad aws vpc_names: %+v", aws.VPCNames)
	}
	if aws.SecurityGroupName != "my-sg" {
		t.Fatalf("sg = %q", aws.SecurityGroupName)
	}
	aliyun := &struct {
		VPCID      string   `yaml:"vpc_id"`
		VSwitchIDs []string `yaml:"vswitch_ids"`
	}{}
	if err := c.Section("aliyun", aliyun); err != nil {
		t.Fatalf("Section(aliyun): %v", err)
	}
	if aliyun.VPCID != "vpc-bbb" || len(aliyun.VSwitchIDs) != 2 {
		t.Fatalf("bad aliyun: %+v", aliyun)
	}
	if c.Cloud() != "aws" || c.Region() != "cn-hangzhou" || !c.UseSpot() {
		t.Fatalf("accessors: cloud=%q region=%q spot=%v", c.Cloud(), c.Region(), c.UseSpot())
	}
}

func TestUserOverlayProject(t *testing.T) {
	dir := t.TempDir()
	writeTmp(t, dir, FileName, `
aws:
  vpc_names: [vpc-user]
  security_group_name: user-sg
aliyun:
  vpc_id: vpc-user
      `)
	userPath := filepath.Join(dir, FileName)
	SetPath(userPath)
	defer Reset()

	projDir := filepath.Join(dir, "proj")
	writeTmp(t, projDir, ProjectFileName, `
aws:
  vpc_names: [vpc-proj]
aliyun:
  vpc_id: vpc-proj
  security_group_id: sg-proj
region: us-east-1
`)
	wd, _ := os.Getwd()
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}

	c := Load()
	// Project VPCNames replaces user's wholesale; security group stays from user.
	aws := awsSec(t, c)
	if len(aws.VPCNames) != 1 || aws.VPCNames[0] != "vpc-proj" {
		t.Fatalf("project should override vpc names, got %+v", aws.VPCNames)
	}
	if aws.SecurityGroupName != "user-sg" {
		t.Fatalf("user sg should survive, got %q", aws.SecurityGroupName)
	}
	aliyun := &struct {
		VPCID           string `yaml:"vpc_id"`
		SecurityGroupID string `yaml:"security_group_id"`
	}{}
	if err := c.Section("aliyun", aliyun); err != nil {
		t.Fatalf("Section(aliyun): %v", err)
	}
	if aliyun.VPCID != "vpc-proj" || aliyun.SecurityGroupID != "sg-proj" {
		t.Fatalf("aliyun project override failed: %+v", aliyun)
	}
	if c.Region() != "us-east-1" {
		t.Fatalf("region = %q", c.Region())
	}
}

func TestAbsentFilesEmpty(t *testing.T) {
	dir := t.TempDir()
	SetPath(filepath.Join(dir, FileName))
	defer Reset()

	c := Load()
	if c == nil {
		t.Fatal("Load should return non-nil for absent files")
	}
	if c.Cloud() != "" {
		t.Fatalf("expected empty config, got %+v", c)
	}
	if err := c.Section("aws", &struct{}{}); err == nil {
		t.Fatal("absent section should return ErrNoSection")
	}
}

func TestBrokenFileEmpty(t *testing.T) {
	dir := t.TempDir()
	writeTmp(t, dir, FileName, "aws: [not, [valid")
	SetPath(filepath.Join(dir, FileName))
	defer Reset()

	c := Load()
	if err := c.Section("aws", &struct{}{}); err == nil {
		t.Fatal("broken file should yield no aws section")
	}
}

func TestProjectOnly(t *testing.T) {
	dir := t.TempDir()
	// No user config file: only the project file exists.
	SetPath(filepath.Join(dir, "absent", FileName))
	defer Reset()

	projDir := filepath.Join(dir, "proj")
	writeTmp(t, projDir, ProjectFileName, `
aws:
  subnet_names: [subnet-x]
region: eu-west-1
`)
	wd, _ := os.Getwd()
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}

	c := Load()
	aws := awsSec(t, c)
	if len(aws.SubnetNames) != 1 || aws.SubnetNames[0] != "subnet-x" {
		t.Fatalf("project-only config not loaded: %+v", aws.SubnetNames)
	}
	if c.Region() != "eu-west-1" {
		t.Fatalf("region = %q", c.Region())
	}
}
