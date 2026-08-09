package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Backend abstracts state persistence. Implementations store clusters,
// services and jobs either in local JSON files, SQLite or MySQL, selected
// by configuration (see Config).
type Backend interface {
	LoadClusters() (map[string]*Cluster, error)
	LoadServices() (map[string]*Service, error)
	LoadJobs() (map[string]*Job, error)
	SaveClusters(clusters map[string]*Cluster) error
	SaveServices(services map[string]*Service) error
	SaveJobs(jobs map[string]*Job) error

	LoadClusterYAMLs() (map[string]*ClusterYAML, error)
	SaveClusterYAMLs(yamls map[string]*ClusterYAML) error
	LoadClusterHistory() (map[string]*ClusterHistory, error)
	SaveClusterHistory(history map[string]*ClusterHistory) error
	LoadClusterEvents() ([]*ClusterEvent, error)
	SaveClusterEvents(events []*ClusterEvent) error

	LoadConfig() (map[string]*ConfigEntry, error)
	SaveConfig(entries map[string]*ConfigEntry) error
	LoadTokens() ([]*ServiceAccountToken, error)
	SaveTokens(tokens []*ServiceAccountToken) error

	Close() error
}

// BackendName identifies a supported persistence backend.
type BackendName string

const (
	// BackendFile persists state as local JSON files (default).
	BackendFile BackendName = "file"
	// BackendSQLite persists state in a SQLite database file.
	BackendSQLite BackendName = "sqlite"
	// BackendMySQL persists state in a MySQL database.
	BackendMySQL BackendName = "mysql"
	// BackendRedis persists state in a Redis instance.
	BackendRedis BackendName = "redis"
)

// Config selects the persistence backend and its connection settings.
type Config struct {
	Backend BackendName
	// FilePath is the state.json path used by the file backend.
	FilePath string
	// SQLite is the database file path used by the sqlite backend.
	SQLite string
	// MySQLDSN is the data source name used by the mysql backend,
	// e.g. "user:pass@tcp(host:3306)/gpi".
	MySQLDSN string
	// Redis carries the connection settings for the redis backend.
	Redis RedisConfig
}

// DefaultConfig resolves the backend selection from environment variables:
//
//	GPI_STATE_BACKEND   file | sqlite | mysql | redis (default: file)
//	GPI_STATE_SQLITE    sqlite database path       (default: $GPI_HOME/gpi.db)
//	GPI_STATE_MYSQL_DSN mysql data source name
//	GPI_STATE_REDIS_ADDR redis address             (default: localhost:6379)
//	GPI_STATE_REDIS_PASSWORD redis password
//	GPI_STATE_REDIS_DB    redis logical database   (default: 0)
//
// The file backend path is derived from DefaultDir() (GPI_HOME or ~/.gpi).
func DefaultConfig() Config {
	dir, _ := DefaultDir()
	backend := BackendName(os.Getenv("GPI_STATE_BACKEND"))
	switch backend {
	case BackendSQLite, BackendMySQL, BackendRedis:
	default:
		backend = BackendFile
	}
	redisDB := 0
	if v := os.Getenv("GPI_STATE_REDIS_DB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			redisDB = n
		}
	}
	return Config{
		Backend:  backend,
		FilePath: filepath.Join(dir, "state.json"),
		SQLite:   envOr("GPI_STATE_SQLITE", filepath.Join(dir, "gpi.db")),
		MySQLDSN: os.Getenv("GPI_STATE_MYSQL_DSN"),
		Redis: RedisConfig{
			Addr:     envOr("GPI_STATE_REDIS_ADDR", "localhost:6379"),
			Password: os.Getenv("GPI_STATE_REDIS_PASSWORD"),
			DB:       redisDB,
		},
	}
}

// Open opens the store using the configuration derived from the environment.
func Open() (*Store, error) {
	return OpenConfig(DefaultConfig())
}

// OpenConfig opens a store backed by the given configuration.
func OpenConfig(cfg Config) (*Store, error) {
	backend, err := newBackend(cfg)
	if err != nil {
		return nil, err
	}
	clusters, err := backend.LoadClusters()
	if err != nil {
		backend.Close()
		return nil, err
	}
	services, err := backend.LoadServices()
	if err != nil {
		backend.Close()
		return nil, err
	}
	jobs, err := backend.LoadJobs()
	if err != nil {
		backend.Close()
		return nil, err
	}
	yamls, err := backend.LoadClusterYAMLs()
	if err != nil {
		backend.Close()
		return nil, err
	}
	history, err := backend.LoadClusterHistory()
	if err != nil {
		backend.Close()
		return nil, err
	}
	events, err := backend.LoadClusterEvents()
	if err != nil {
		backend.Close()
		return nil, err
	}
	config, err := backend.LoadConfig()
	if err != nil {
		backend.Close()
		return nil, err
	}
	tokens, err := backend.LoadTokens()
	if err != nil {
		backend.Close()
		return nil, err
	}
	s := &Store{
		backend:        backend,
		clusters:       clusters,
		services:       services,
		jobs:           jobs,
		clusterYAMLs:   yamls,
		clusterHistory: history,
		clusterEvents:  events,
		config:         config,
		tokens:         tokens,
	}
	return s, nil
}

// OpenAt opens a file-backend store rooted at the given state.json path;
// sibling files (state-services.json, state-jobs.json) are loaded alongside.
func OpenAt(path string) (*Store, error) {
	return OpenConfig(Config{Backend: BackendFile, FilePath: path})
}

func newBackend(cfg Config) (Backend, error) {
	switch cfg.Backend {
	case "", BackendFile:
		return &fileBackend{path: cfg.FilePath}, nil
	case BackendSQLite:
		return newSQLBackend("sqlite", cfg.SQLite)
	case BackendMySQL:
		if cfg.MySQLDSN == "" {
			return nil, fmt.Errorf("mysql backend requires GPI_STATE_MYSQL_DSN")
		}
		return newSQLBackend("mysql", cfg.MySQLDSN)
	case BackendRedis:
		return newRedisBackend(cfg.Redis)
	default:
		return nil, fmt.Errorf("unknown state backend %q (want file|sqlite|mysql|redis)", cfg.Backend)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
