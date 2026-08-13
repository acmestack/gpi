package aliyun

import (
	"github.com/acmestack/gpi/internal/config"
)

// Config holds Alibaba Cloud network/launch preferences read from the gpi user
// config's "aliyun" section. Resources are matched by id; matched resources
// are reused instead of auto-created.
type Config struct {
	// VPCID is the VPC to use.
	VPCID string `yaml:"vpc_id"`
	// VSwitchIDs lists vswitches (by id) to prefer.
	VSwitchIDs []string `yaml:"vswitch_ids"`
	// SecurityGroupID is a security group to reuse instead of creating one.
	SecurityGroupID string `yaml:"security_group_id"`
	// SSHUser is the SSH user to log in as.
	SSHUser string `yaml:"ssh_user"`
}

// LoadConfig returns the merged "aliyun" config section, or nil when unset.
func LoadConfig() *Config {
	var c Config
	if err := config.Load().Section(CloudName, &c); err != nil {
		return nil
	}
	return &c
}
