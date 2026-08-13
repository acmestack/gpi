package task

import (
	"fmt"
)

// Credentials holds optional per-task cloud access keys. If provided they are
// used for this task's launch; otherwise gpi falls back to the default
// env/disk credential loading.
//
// Credentials is a provider-agnostic map of per-cloud access keys: the key is
// the cloud name (e.g. "aws", "aliyun", any future cloud) and the value holds
// the access key/secret/region. New clouds reuse this as-is — no per-cloud
// struct or switch needed.
type Credentials struct {
	// Clouds maps a cloud name to its credentials, e.g.
	// `credentials: { aws: { access_key_id: ..., secret_access_key: ... } }`.
	Clouds map[string]*CloudCredentials `yaml:"credentials,omitempty" json:"credentials,omitempty"`
}

// UnmarshalYAML parses credentials as a cloud-name → keys mapping, accepting
// either secret_access_key or access_key_secret for the secret field.
func (c *Credentials) UnmarshalYAML(unmarshal func(any) error) error {
	type credEntry struct {
		AccessKeyID     string `yaml:"access_key_id"`
		SecretAccessKey string `yaml:"secret_access_key,omitempty"`
		AccessKeySecret string `yaml:"access_key_secret,omitempty"`
		Region          string `yaml:"region,omitempty"`
	}
	raw := map[string]credEntry{}
	if err := unmarshal(&raw); err != nil {
		return err
	}
	c.Clouds = make(map[string]*CloudCredentials, len(raw))
	for name, r := range raw {
		secret := r.SecretAccessKey
		if secret == "" {
			secret = r.AccessKeySecret
		}
		c.Clouds[name] = &CloudCredentials{
			AccessKeyID:     r.AccessKeyID,
			SecretAccessKey: secret,
			Region:          r.Region,
		}
	}
	return nil
}

// MarshalYAML renders credentials as a cloud-name → keys mapping (secret field
// as secret_access_key).
func (c *Credentials) MarshalYAML() (any, error) {
	out := map[string]any{}
	for name, cc := range c.Clouds {
		out[name] = map[string]any{
			"access_key_id":     cc.AccessKeyID,
			"secret_access_key": cc.SecretAccessKey,
			"region":            cc.Region,
		}
	}
	return out, nil
}

// Validate ensures every provided cloud credential block has both an access
// key id and a secret.
func (c *Credentials) Validate() error {
	if c == nil {
		return nil
	}
	for name, cc := range c.Clouds {
		if cc.AccessKeyID == "" || cc.SecretAccessKey == "" {
			return fmt.Errorf("%s credentials require access_key_id and secret_access_key", name)
		}
	}
	return nil
}

// ForCloud returns the cloud-level credentials for the given provider name,
// or nil if no matching credential block was supplied.
func (c *Credentials) ForCloud(name string) *CloudCredentials {
	if c == nil || c.Clouds == nil {
		return nil
	}
	return c.Clouds[name]
}

// CloudCredentials is a provider-agnostic view used by the provisioner.
type CloudCredentials struct {
	// AccessKeyID is the cloud access key id (e.g. AWS Access Key, Aliyun
	// AccessKey ID).
	AccessKeyID string `yaml:"access_key_id" json:"accessKeyId"`
	// SecretAccessKey is the cloud secret key (e.g. AWS Secret Access Key,
	// Aliyun AccessKey Secret).
	SecretAccessKey string `yaml:"secret_access_key" json:"secretAccessKey"`
	// Region is the optional cloud region the credentials are scoped to.
	Region string `yaml:"region,omitempty" json:"region,omitempty"`
}
