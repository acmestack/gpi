package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// fileBackend persists the state collections as sibling JSON files
// (state.json, state-services.json, state-jobs.json, state-cluster-yaml.json,
// state-cluster-history.json, state-cluster-events.json). Writes are atomic
// (tmp file + rename).
type fileBackend struct {
	path string
}

func (b *fileBackend) LoadClusters() (map[string]*Cluster, error) {
	m := map[string]*Cluster{}
	if data, err := os.ReadFile(b.path); err == nil {
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("corrupt state file %s: %w", b.path, err)
		}
	}
	return m, nil
}

func (b *fileBackend) LoadServices() (map[string]*Service, error) {
	return loadFileMap[*Service](b.sibling("services"))
}

func (b *fileBackend) LoadJobs() (map[string]*Job, error) {
	return loadFileMap[*Job](b.sibling("jobs"))
}

func (b *fileBackend) LoadClusterYAMLs() (map[string]*ClusterYAML, error) {
	return loadFileMap[*ClusterYAML](b.sibling("cluster-yaml"))
}

func (b *fileBackend) LoadClusterHistory() (map[string]*ClusterHistory, error) {
	return loadFileMap[*ClusterHistory](b.sibling("cluster-history"))
}

func (b *fileBackend) LoadClusterEvents() ([]*ClusterEvent, error) {
	out := []*ClusterEvent{}
	if data, err := os.ReadFile(b.sibling("cluster-events")); err == nil {
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("corrupt events file %s: %w", b.sibling("cluster-events"), err)
		}
	}
	return out, nil
}

func (b *fileBackend) LoadConfig() (map[string]*ConfigEntry, error) {
	return loadFileMap[*ConfigEntry](b.sibling("config"))
}

func (b *fileBackend) LoadTokens() ([]*ServiceAccountToken, error) {
	out := []*ServiceAccountToken{}
	if data, err := os.ReadFile(b.sibling("tokens")); err == nil {
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("corrupt tokens file %s: %w", b.sibling("tokens"), err)
		}
	}
	return out, nil
}

func (b *fileBackend) SaveClusters(clusters map[string]*Cluster) error {
	return writeFileMap(b.path, clusters)
}

func (b *fileBackend) SaveServices(services map[string]*Service) error {
	return writeFileMap(b.sibling("services"), services)
}

func (b *fileBackend) SaveJobs(jobs map[string]*Job) error {
	return writeFileMap(b.sibling("jobs"), jobs)
}

func (b *fileBackend) SaveClusterYAMLs(yamls map[string]*ClusterYAML) error {
	return writeFileMap(b.sibling("cluster-yaml"), yamls)
}

func (b *fileBackend) SaveClusterHistory(history map[string]*ClusterHistory) error {
	return writeFileMap(b.sibling("cluster-history"), history)
}

func (b *fileBackend) SaveClusterEvents(events []*ClusterEvent) error {
	return writeFileSlice(b.sibling("cluster-events"), events)
}

func (b *fileBackend) SaveConfig(entries map[string]*ConfigEntry) error {
	return writeFileMap(b.sibling("config"), entries)
}

func (b *fileBackend) SaveTokens(tokens []*ServiceAccountToken) error {
	return writeFileSlice(b.sibling("tokens"), tokens)
}

func (b *fileBackend) Close() error { return nil }

func (b *fileBackend) sibling(suffix string) string {
	ext := filepath.Ext(b.path)
	base := b.path
	if ext != "" {
		base = b.path[:len(b.path)-len(ext)]
	}
	return base + "-" + suffix + ext
}

func loadFileMap[T any](path string) (map[string]T, error) {
	m := map[string]T{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("corrupt state file %s: %w", path, err)
		}
	}
	return m, nil
}

func writeFileMap[T any](path string, m map[string]T) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, data)
}

func writeFileSlice[T any](path string, s []T) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, data)
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
