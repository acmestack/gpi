// Package config loads gpi's user configuration from YAML files, mirroring
// SkyPilot's ~/.sky/config.yaml. A user-level config ($GPI_HOME/config.yaml,
// default ~/.gpi/config.yaml) is layered with an optional project-level config
// (.gpi.yaml in the working directory), with the project file overriding the
// user file. The config carries generic runtime defaults and per-cloud launch
// preferences (VPC/subnet/security group reuse); CLI flags take precedence
// over these values when explicitly set.
//
// Per-cloud sections are opaque to this package: each cloud defines its own
// config struct and decodes it via Config.Section, so adding a cloud never
// requires changes here.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// FileName is the user-level config file name inside the gpi home dir.
const FileName = "config.yaml"

// ProjectFileName is the project-level config file read from the working dir.
const ProjectFileName = ".gpi.yaml"

// ErrNoSection is returned by Section when the requested cloud section is
// absent from the merged config.
var ErrNoSection = errors.New("config: no such section")

// Config is the top-level parsed configuration. Cloud-specific sections
// (aws, aliyun, ...) are not typed here; decode them with Section.
type Config struct {
	// AllowedClouds limits optimization to the listed clouds.
	AllowedClouds []string `yaml:"allowed_clouds"`
	// DefaultRegion is the region used when neither task nor flag pins one.
	DefaultRegion string `yaml:"region"`
	// DefaultZone is the zone used when neither task nor flag pins one.
	DefaultZone string `yaml:"zone"`
	// DefaultSpot is the spot default applied unless the task/flag says otherwise.
	DefaultSpot *bool `yaml:"use_spot"`

	// node is the merged config tree, kept for per-cloud Section decoding.
	node *yaml.Node
}

var (
	mu   sync.Mutex
	path string
	cfg  *Config
)

// SetPath overrides the user-level config file path for tests.
func SetPath(p string) {
	mu.Lock()
	defer mu.Unlock()
	path = p
	cfg = nil
}

// Reset clears the cached config, forcing a reload on next access.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	cfg = nil
}

// HomeDir returns the gpi home directory, honoring GPI_HOME and falling back
// to ~/.gpi, mirroring state.DefaultDir.
func HomeDir() (string, error) {
	if env := os.Getenv("GPI_HOME"); env != "" {
		return env, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gpi"), nil
}

// UserPath returns the resolved user-level config file path.
func UserPath() (string, error) {
	if path != "" {
		return path, nil
	}
	dir, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

// ProjectPath returns the project-level config path for the working directory
// (.gpi.yaml), regardless of whether it exists.
func ProjectPath() string {
	wd, _ := os.Getwd()
	return filepath.Join(wd, ProjectFileName)
}

// Load returns the merged configuration. The user-level config is loaded from
// UserPath and overlaid with the project-level config when it exists; an
// absent file yields an empty Config. The result is cached until Reset.
func Load() *Config {
	mu.Lock()
	defer mu.Unlock()
	if cfg != nil {
		return cfg
	}
	cfg = load()
	return cfg
}

func load() *Config {
	var base *yaml.Node
	if user, err := UserPath(); err == nil {
		base = parseNode(user)
	}
	if proj := parseNode(ProjectPath()); proj != nil {
		base = mergeNode(base, proj)
	}
	c := &Config{}
	if base != nil {
		_ = base.Decode(c)
		c.node = base
	}
	return c
}

// parseNode reads and parses a single config file into a mapping node; it
// returns nil when the file does not exist, is not a mapping, or fails to
// parse (a broken file is treated as absent so gpi still works).
func parseNode(file string) *yaml.Node {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return nil
		}
		doc = *doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	return &doc
}

// mergeNode overlays overlay on top of base, mutating base: keys in overlay
// replace base's value, except when both values are mappings (recursively
// merged) so per-cloud sections layer field by field. base nil returns overlay.
func mergeNode(base, overlay *yaml.Node) *yaml.Node {
	if base == nil {
		return overlay
	}
	out := *base
	children := make([]*yaml.Node, 0, len(base.Content)+len(overlay.Content))
	byKey := make(map[string]int, len(base.Content)/2)
	for i := 0; i+1 < len(out.Content); i += 2 {
		byKey[out.Content[i].Value] = len(children)
		children = append(children, out.Content[i], out.Content[i+1])
	}
	for i := 0; i+1 < len(overlay.Content); i += 2 {
		k, v := overlay.Content[i], overlay.Content[i+1]
		if idx, ok := byKey[k.Value]; ok {
			if children[idx+1].Kind == yaml.MappingNode && v.Kind == yaml.MappingNode {
				children[idx+1] = mergeNode(children[idx+1], v)
			} else {
				children[idx+1] = v
			}
		} else {
			byKey[k.Value] = len(children)
			children = append(children, k, v)
		}
	}
	out.Content = children
	return &out
}

// Section decodes the per-cloud section named name (e.g. "aws") into out,
// which should point to the cloud's own config struct. It returns ErrNoSection
// when the section is absent, or the yaml decode error when it is malformed.
func (c *Config) Section(name string, out any) error {
	if c == nil || c.node == nil {
		return ErrNoSection
	}
	for i := 0; i+1 < len(c.node.Content); i += 2 {
		if c.node.Content[i].Value == name {
			return c.node.Content[i+1].Decode(out)
		}
	}
	return ErrNoSection
}

// Cloud returns the default cloud filter (comma-joined AllowedClouds), or ""
// when unset.
func (c *Config) Cloud() string {
	if c == nil || len(c.AllowedClouds) == 0 {
		return ""
	}
	return strings.Join(c.AllowedClouds, ",")
}

// Region returns the default region, or "" when unset.
func (c *Config) Region() string {
	if c == nil {
		return ""
	}
	return c.DefaultRegion
}

// Zone returns the default zone, or "" when unset.
func (c *Config) Zone() string {
	if c == nil {
		return ""
	}
	return c.DefaultZone
}

// UseSpot returns the default spot preference, or false when unset.
func (c *Config) UseSpot() bool {
	if c == nil || c.DefaultSpot == nil {
		return false
	}
	return *c.DefaultSpot
}
