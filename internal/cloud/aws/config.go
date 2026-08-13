package aws

import (
	"github.com/acmestack/gpi/internal/config"
)

// Config holds AWS network/launch preferences read from the gpi user config's
// "aws" section. VPC, subnet and security group values are matched by Name
// where present, else by id; matched resources are reused instead of
// auto-created.
type Config struct {
	// VPCNames lists VPCs (by Name tag or id) to prefer. The first existing
	// match is used; when none match, the default VPC is used.
	VPCNames []string `yaml:"vpc_names"`
	// SubnetNames lists subnets (by Name tag or id) to prefer.
	SubnetNames []string `yaml:"subnet_names"`
	// SecurityGroupName is a security group (by name or id) to reuse instead
	// of creating gpi-sg.
	SecurityGroupName string `yaml:"security_group_name"`
	// SSHUser is the SSH user to log in as.
	SSHUser string `yaml:"ssh_user"`
}

// LoadConfig returns the merged "aws" config section, or nil when unset.
func LoadConfig() *Config {
	var c Config
	if err := config.Load().Section(CloudName, &c); err != nil {
		return nil
	}
	return &c
}
