package state

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisBackend persists each state collection as a JSON blob under its own
// Redis key (gpi:clusters, gpi:services, ...), mirroring the file backend's
// per-collection files. Writes are SET (atomic by nature of a single key).
type redisBackend struct {
	client *redis.Client
	prefix string
}

const (
	redisPrefix       = "gpi:"
	redisPingTimeout  = 5 * time.Second
	redisDefaultAddr  = "localhost:6379"
	redisDefaultDB    = 0
	redisDefaultProto = 3
)

// RedisConfig carries the connection settings for the redis backend.
type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

// newRedisBackend opens a client and verifies connectivity.
func newRedisBackend(cfg RedisConfig) (*redisBackend, error) {
	addr := cfg.Addr
	if addr == "" {
		addr = redisDefaultAddr
	}
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: cfg.Password,
		DB:       cfg.DB,
		Protocol: redisDefaultProto,
	})
	ctx, cancel := context.WithTimeout(context.Background(), redisPingTimeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("ping redis backend (%s): %w", addr, err)
	}
	return &redisBackend{client: client, prefix: redisPrefix}, nil
}

func (b *redisBackend) key(name string) string { return b.prefix + name }

func (b *redisBackend) load(ctx context.Context, name string, v any) error {
	data, err := b.client.Get(ctx, b.key(name)).Bytes()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return fmt.Errorf("redis get %s: %w", name, err)
	}
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("corrupt redis key %s: %w", name, err)
	}
	return nil
}

func (b *redisBackend) save(ctx context.Context, name string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := b.client.Set(ctx, b.key(name), data, 0).Err(); err != nil {
		return fmt.Errorf("redis set %s: %w", name, err)
	}
	return nil
}

// LoadClusters loads all clusters from Redis.
func (b *redisBackend) LoadClusters() (map[string]*Cluster, error) {
	m := map[string]*Cluster{}
	if err := b.load(context.Background(), "clusters", &m); err != nil {
		return nil, err
	}
	return m, nil
}

// LoadServices loads all services from Redis.
func (b *redisBackend) LoadServices() (map[string]*Service, error) {
	m := map[string]*Service{}
	if err := b.load(context.Background(), "services", &m); err != nil {
		return nil, err
	}
	return m, nil
}

// LoadJobs loads all jobs from Redis.
func (b *redisBackend) LoadJobs() (map[string]*Job, error) {
	m := map[string]*Job{}
	if err := b.load(context.Background(), "jobs", &m); err != nil {
		return nil, err
	}
	return m, nil
}

// LoadClusterYAMLs loads all cluster task YAMLs from Redis.
func (b *redisBackend) LoadClusterYAMLs() (map[string]*ClusterYAML, error) {
	m := map[string]*ClusterYAML{}
	if err := b.load(context.Background(), "cluster_yaml", &m); err != nil {
		return nil, err
	}
	return m, nil
}

// LoadClusterHistory loads all cluster history records from Redis.
func (b *redisBackend) LoadClusterHistory() (map[string]*ClusterHistory, error) {
	m := map[string]*ClusterHistory{}
	if err := b.load(context.Background(), "cluster_history", &m); err != nil {
		return nil, err
	}
	return m, nil
}

// LoadClusterEvents loads all cluster events from Redis.
func (b *redisBackend) LoadClusterEvents() ([]*ClusterEvent, error) {
	out := []*ClusterEvent{}
	if err := b.load(context.Background(), "cluster_events", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// LoadConfig loads all config entries from Redis.
func (b *redisBackend) LoadConfig() (map[string]*ConfigEntry, error) {
	m := map[string]*ConfigEntry{}
	if err := b.load(context.Background(), "config", &m); err != nil {
		return nil, err
	}
	return m, nil
}

// LoadTokens loads all service account tokens from Redis.
func (b *redisBackend) LoadTokens() ([]*ServiceAccountToken, error) {
	out := []*ServiceAccountToken{}
	if err := b.load(context.Background(), "tokens", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SaveClusters persists all clusters to Redis.
func (b *redisBackend) SaveClusters(clusters map[string]*Cluster) error {
	return b.save(context.Background(), "clusters", clusters)
}

// SaveServices persists all services to Redis.
func (b *redisBackend) SaveServices(services map[string]*Service) error {
	return b.save(context.Background(), "services", services)
}

// SaveJobs persists all jobs to Redis.
func (b *redisBackend) SaveJobs(jobs map[string]*Job) error {
	return b.save(context.Background(), "jobs", jobs)
}

// SaveClusterYAMLs persists all cluster task YAMLs to Redis.
func (b *redisBackend) SaveClusterYAMLs(yamls map[string]*ClusterYAML) error {
	return b.save(context.Background(), "cluster_yaml", yamls)
}

// SaveClusterHistory persists all cluster history records to Redis.
func (b *redisBackend) SaveClusterHistory(history map[string]*ClusterHistory) error {
	return b.save(context.Background(), "cluster_history", history)
}

// SaveClusterEvents persists all cluster events to Redis.
func (b *redisBackend) SaveClusterEvents(events []*ClusterEvent) error {
	return b.save(context.Background(), "cluster_events", events)
}

// SaveConfig persists all config entries to Redis.
func (b *redisBackend) SaveConfig(entries map[string]*ConfigEntry) error {
	return b.save(context.Background(), "config", entries)
}

// SaveTokens persists all service account tokens to Redis.
func (b *redisBackend) SaveTokens(tokens []*ServiceAccountToken) error {
	return b.save(context.Background(), "tokens", tokens)
}

// Close releases the underlying Redis client connection.
func (b *redisBackend) Close() error { return b.client.Close() }
