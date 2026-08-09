package state

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

func TestStoreRoundTripFile(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenAt(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	exerciseStore(t, store)
	store.Close()
}

func TestStoreRoundTripSQLite(t *testing.T) {
	store, err := OpenConfig(Config{
		Backend: BackendSQLite,
		SQLite:  filepath.Join(t.TempDir(), "gpi.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	exerciseStore(t, store)
	store.Close()
}

func TestUnknownBackend(t *testing.T) {
	if _, err := OpenConfig(Config{Backend: BackendName("unknown")}); err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

func TestStoreRoundTripRedis(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	store, err := OpenConfig(Config{
		Backend: BackendRedis,
		Redis:   RedisConfig{Addr: mr.Addr()},
	})
	if err != nil {
		t.Fatal(err)
	}
	exerciseStore(t, store)
	store.Close()
}

func TestRedisReopen(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	cfg := Config{Backend: BackendRedis, Redis: RedisConfig{Addr: mr.Addr()}}
	store, err := OpenConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	job := &Job{Name: "j1", Schedule: "@daily", Retries: 2}
	if err := store.AddJob(job); err != nil {
		t.Fatal(err)
	}
	store.Close()

	store, err = OpenConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if got, err := store.GetJob("j1"); err != nil || got == nil {
		t.Fatalf("expected job to persist across reopen, got %v err %v", got, err)
	}
}

// reopenStore re-opens a store using the same backend config as s.
func reopenStore(t *testing.T, s *Store) *Store {
	t.Helper()
	var backend Backend
	var err error
	switch b := s.backend.(type) {
	case *fileBackend:
		backend, err = newBackend(Config{Backend: BackendFile, FilePath: b.path})
	case *sqlBackend:
		backend, err = b.reopen()
	case *redisBackend:
		backend, err = newRedisBackend(RedisConfig{Addr: b.client.Options().Addr})
	default:
		t.Fatal("unknown backend type")
	}
	if err != nil {
		t.Fatal(err)
	}
	clusters, err := backend.LoadClusters()
	if err != nil {
		t.Fatal(err)
	}
	services, err := backend.LoadServices()
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := backend.LoadJobs()
	if err != nil {
		t.Fatal(err)
	}
	yamls, err := backend.LoadClusterYAMLs()
	if err != nil {
		t.Fatal(err)
	}
	history, err := backend.LoadClusterHistory()
	if err != nil {
		t.Fatal(err)
	}
	events, err := backend.LoadClusterEvents()
	if err != nil {
		t.Fatal(err)
	}
	config, err := backend.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := backend.LoadTokens()
	if err != nil {
		t.Fatal(err)
	}
	return &Store{
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
}

func exerciseStore(t *testing.T, s *Store) {
	t.Helper()

	c := &Cluster{Name: "demo", Cloud: "aws", Region: "us-east-1", NumNodes: 2,
		Instances: []Node{{ID: "i-1", Role: RoleHead, PublicIP: "1.2.3.4"}}}
	if err := s.AddCluster(c); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetCluster("demo")
	if err != nil {
		t.Fatal(err)
	}
	if got.Cloud != "aws" || got.NumNodes != 2 || len(got.Instances) != 1 {
		t.Fatalf("bad cluster: %+v", got)
	}
	if err := s.UpdateCluster("demo", func(cl *Cluster) error { cl.Region = "eu-west-1"; return nil }); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetCluster("demo")
	if got.Region != "eu-west-1" {
		t.Fatalf("update failed: %+v", got)
	}

	svc := &Service{Name: "srv", Replicas: 2, Port: 8080}
	if err := s.AddService(svc); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetService("srv"); err != nil {
		t.Fatal(err)
	}

	job := &Job{Name: "j1", Schedule: "@daily", Retries: 2}
	if err := s.AddJob(job); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetJob("j1"); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteCluster("demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetCluster("demo"); err == nil {
		t.Fatal("cluster should be deleted")
	}
	if err := s.DeleteService("srv"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetService("srv"); err == nil {
		t.Fatal("service should be deleted")
	}

	// Re-open and confirm jobs persisted, clusters/services empty.
	s.Close()
	reopened := reopenStore(t, s)
	if len(reopened.ListJobs()) != 1 {
		t.Fatalf("job should persist, got %d", len(reopened.ListJobs()))
	}
	if len(reopened.ListClusters()) != 0 {
		t.Fatalf("clusters should be empty, got %d", len(reopened.ListClusters()))
	}
	if len(reopened.ListServices()) != 0 {
		t.Fatalf("services should be empty, got %d", len(reopened.ListServices()))
	}
	reopened.Close()
}

func TestStoreTimestampsPreserved(t *testing.T) {
	store, err := OpenConfig(Config{Backend: BackendSQLite, SQLite: filepath.Join(t.TempDir(), "t.db")})
	if err != nil {
		t.Fatal(err)
	}
	job := &Job{Name: "j", Schedule: ""}
	if err := store.AddJob(job); err != nil {
		t.Fatal(err)
	}
	created := job.CreatedAt
	store.Close()

	reopened := reopenStore(t, store)
	j, _ := reopened.GetJob("j")
	if j.CreatedAt != created {
		t.Fatalf("created_at not preserved: got %d want %d", j.CreatedAt, created)
	}
	reopened.Close()
}

func TestStorePersistsAcrossReopenSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persist.db")
	store, err := OpenConfig(Config{Backend: BackendSQLite, SQLite: path})
	if err != nil {
		t.Fatal(err)
	}
	c := &Cluster{Name: "keep", Cloud: "aliyun"}
	if err := store.AddCluster(c); err != nil {
		t.Fatal(err)
	}
	store.Close()

	store2, err := OpenConfig(Config{Backend: BackendSQLite, SQLite: path})
	if err != nil {
		t.Fatal(err)
	}
	defer store2.Close()
	got, err := store2.GetCluster("keep")
	if err != nil {
		t.Fatal(err)
	}
	if got.Cloud != "aliyun" {
		t.Fatalf("cluster not persisted: %+v", got)
	}
}

func TestMigrateLegacyTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	// Create a legacy single-table DB, bypassing the new backend.
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE csp_state (
		kind VARCHAR(32) NOT NULL,
		name VARCHAR(255) NOT NULL,
		data MEDIUMTEXT NOT NULL,
		created_at BIGINT NOT NULL,
		updated_at BIGINT NOT NULL,
		PRIMARY KEY (kind, name))`); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO csp_state (kind, name, data, created_at, updated_at)
		VALUES ('clusters', 'legacy-cluster', '{"name":"legacy-cluster","cloud":"aws","num_nodes":3}', 111, 222)`); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO csp_state (kind, name, data, created_at, updated_at)
		VALUES ('jobs', 'legacy-job', '{"name":"legacy-job","schedule":"@daily"}', 333, 444)`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	// Open with the new backend: migration moves data into per-entity tables.
	b, err := newSQLBackend("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	clusters, err := b.LoadClusters()
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 1 || clusters["legacy-cluster"].Cloud != "aws" {
		t.Fatalf("clusters after migrate = %+v", clusters)
	}
	jobs, err := b.LoadJobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs["legacy-job"].Schedule != "@daily" {
		t.Fatalf("jobs after migrate = %+v", jobs)
	}

	// Legacy table should be dropped.
	var count int
	if err := b.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='csp_state'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("legacy csp_state table still present")
	}
}

func TestClusterYAMLHistoryEventsRoundTrip(t *testing.T) {
	store, err := OpenConfig(Config{Backend: BackendSQLite, SQLite: filepath.Join(t.TempDir(), "t.db")})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.RecordClusterYAML("demo", "name: demo\nrun: echo hi\n"); err != nil {
		t.Fatal(err)
	}
	y, err := store.GetClusterYAML("demo")
	if err != nil || y.YAML == "" {
		t.Fatalf("get cluster yaml = %v, err = %v", y, err)
	}

	if err := store.RecordClusterHistory(&ClusterHistory{
		ClusterName: "demo", NumNodes: 2, Cloud: "aws", Region: "us-east-1",
	}); err != nil {
		t.Fatal(err)
	}
	hist := store.ListClusterHistory()
	if len(hist) != 1 || hist[0].NumNodes != 2 {
		t.Fatalf("history = %+v", hist)
	}

	if err := store.AddClusterEvent(&ClusterEvent{
		ClusterName: "demo", StartingStatus: string(ClusterProvisioning),
		EndingStatus: string(ClusterUp), Type: EventStatusChange,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AddClusterEvent(&ClusterEvent{
		ClusterName: "demo", StartingStatus: string(ClusterUp),
		EndingStatus: string(ClusterDown), Type: EventDown,
	}); err != nil {
		t.Fatal(err)
	}
	events := store.ListClusterEventsFor("demo")
	if len(events) != 2 {
		t.Fatalf("events = %+v", events)
	}
	if events[1].EndingStatus != string(ClusterDown) {
		t.Fatalf("last event = %+v", events[1])
	}
	store.Close()

	// Persistence across reopen for all three collections.
	reopened := reopenStore(t, store)
	if _, err := reopened.GetClusterYAML("demo"); err != nil {
		t.Fatalf("yaml not persisted: %v", err)
	}
	if len(reopened.ListClusterHistory()) != 1 {
		t.Fatalf("history not persisted")
	}
	if len(reopened.ListClusterEvents()) != 2 {
		t.Fatalf("events not persisted")
	}
	reopened.Close()
}

func TestConfigStore(t *testing.T) {
	store, err := OpenConfig(Config{Backend: BackendSQLite, SQLite: filepath.Join(t.TempDir(), "cfg.db")})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetConfig("autostop", "true"); err != nil {
		t.Fatal(err)
	}
	if got := store.GetConfig("autostop"); got != "true" {
		t.Fatalf("get config = %q", got)
	}
	if err := store.SetConfig("autostop", "false"); err != nil {
		t.Fatal(err)
	}
	if got := store.GetConfig("autostop"); got != "false" {
		t.Fatalf("updated config = %q", got)
	}
	if got := store.GetConfig("missing"); got != "" {
		t.Fatalf("missing config = %q", got)
	}
	store.Close()
	reopened := reopenStore(t, store)
	if got := reopened.GetConfig("autostop"); got != "false" {
		t.Fatalf("config not persisted: %q", got)
	}
	reopened.Close()
}

func TestTokenStore(t *testing.T) {
	store, err := OpenConfig(Config{Backend: BackendSQLite, SQLite: filepath.Join(t.TempDir(), "tok.db")})
	if err != nil {
		t.Fatal(err)
	}
	tok, secret, err := store.CreateToken("ci", "qicz", 0)
	if err != nil {
		t.Fatal(err)
	}
	if secret == "" || tok.TokenID == "" {
		t.Fatalf("bad token: %+v secret=%q", tok, secret)
	}
	// Lookup by hash succeeds.
	found, err := store.GetTokenByHash(TokenHash(secret))
	if err != nil || found.TokenID != tok.TokenID {
		t.Fatalf("lookup by hash failed: %v", err)
	}
	// Wrong hash fails.
	if _, err := store.GetTokenByHash("deadbeef"); err == nil {
		t.Fatal("wrong hash should fail")
	}
	// Rotate invalidates old secret.
	_, newSecret, err := store.RotateToken(tok.TokenID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetTokenByHash(TokenHash(secret)); err == nil {
		t.Fatal("old secret should be invalid after rotation")
	}
	if _, err := store.GetTokenByHash(TokenHash(newSecret)); err != nil {
		t.Fatalf("new secret should be valid: %v", err)
	}
	// Delete revokes.
	ok, err := store.DeleteToken(tok.TokenID)
	if err != nil || !ok {
		t.Fatalf("delete = %v, %v", ok, err)
	}
	if _, err := store.GetTokenByHash(TokenHash(newSecret)); err == nil {
		t.Fatal("deleted token should be invalid")
	}
	// Expired token is rejected.
	tok2, secret2, err := store.CreateToken("expired", "qicz", 1)
	if err != nil {
		t.Fatal(err)
	}
	_ = tok2
	_ = secret2
	if _, err := store.GetTokenByHash(TokenHash(secret2)); err == nil {
		t.Fatal("expired token should be rejected")
	}
	store.Close()
}
