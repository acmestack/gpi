package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

// sqlBackend persists each state collection in its own table. Every table has
// an explicit primary key (the entity name) plus a few indexed columns for the
// most common lookups/filters; the full entity is stored as a JSON blob in the
// `data` column. This mirrors SkyPilot's per-entity tables (spot / job_info /
// job_events) where structured fields are indexed and rich content lives in
// Text/JSON columns.
type sqlBackend struct {
	db     *sql.DB
	driver string
	dsn    string
}

const (
	clustersTable = "clusters"
	servicesTable = "services"
	jobsTable     = "jobs"

	clusterYAMLTable    = "cluster_yaml"
	clusterHistoryTable = "cluster_history"
	clusterEventsTable  = "cluster_events"

	configTable = "config"
	tokensTable = "service_account_tokens"
)

func newSQLBackend(driver, dsn string) (*sqlBackend, error) {
	if driver == "mysql" && !strings.Contains(dsn, "parseTime=") {
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		dsn += sep + "parseTime=true"
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s backend: %w", driver, err)
	}
	// modernc sqlite: allow concurrent access; keep busy-timeout default.
	if driver == "sqlite" {
		db.SetMaxOpenConns(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping %s backend: %w", driver, err)
	}
	b := &sqlBackend{db: db, driver: driver, dsn: dsn}
	if err := b.ensureTables(); err != nil {
		db.Close()
		return nil, err
	}
	return b, nil
}

// reopen creates a new backend with the same driver/DSN, used by tests.
func (b *sqlBackend) reopen() (*sqlBackend, error) {
	return newSQLBackend(b.driver, b.dsn)
}

// ensureTables creates the per-entity tables if missing and migrates any
// legacy single-table (csp_state) data into them.
func (b *sqlBackend) ensureTables() error {
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS clusters (
			name VARCHAR(255) NOT NULL,
			status VARCHAR(64),
			backend VARCHAR(32),
			cloud VARCHAR(64),
			region VARCHAR(64),
			num_nodes INTEGER NOT NULL DEFAULT 0,
			task_yaml MEDIUMTEXT,
			data MEDIUMTEXT NOT NULL,
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL,
			PRIMARY KEY (name)
		)`,
		`CREATE TABLE IF NOT EXISTS services (
			name VARCHAR(255) NOT NULL,
			status VARCHAR(64),
			replicas INTEGER NOT NULL DEFAULT 0,
			port INTEGER NOT NULL DEFAULT 0,
			data MEDIUMTEXT NOT NULL,
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL,
			PRIMARY KEY (name)
		)`,
		`CREATE TABLE IF NOT EXISTS jobs (
			name VARCHAR(255) NOT NULL,
			status VARCHAR(64),
			schedule VARCHAR(255),
			run_count INTEGER NOT NULL DEFAULT 0,
			fail_count INTEGER NOT NULL DEFAULT 0,
			next_run BIGINT NOT NULL DEFAULT 0,
			data MEDIUMTEXT NOT NULL,
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL,
			PRIMARY KEY (name)
		)`,
		`CREATE TABLE IF NOT EXISTS cluster_yaml (
			cluster_name VARCHAR(255) NOT NULL,
			yaml MEDIUMTEXT NOT NULL,
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL,
			PRIMARY KEY (cluster_name)
		)`,
		`CREATE TABLE IF NOT EXISTS cluster_history (
			cluster_name VARCHAR(255) NOT NULL,
			num_nodes INTEGER NOT NULL DEFAULT 0,
			cloud VARCHAR(64),
			region VARCHAR(64),
			zone VARCHAR(64),
			instance_type VARCHAR(64),
			backend VARCHAR(32),
			task_yaml MEDIUMTEXT,
			launched_at BIGINT NOT NULL DEFAULT 0,
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL,
			PRIMARY KEY (cluster_name)
		)`,
		`CREATE TABLE IF NOT EXISTS cluster_events (
			cluster_name VARCHAR(255) NOT NULL,
			starting_status VARCHAR(64),
			ending_status VARCHAR(64),
			reason MEDIUMTEXT,
			type VARCHAR(32),
			request_id VARCHAR(255),
			transitioned_at BIGINT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS config (
			config_key VARCHAR(255) NOT NULL,
			config_value MEDIUMTEXT NOT NULL,
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL,
			PRIMARY KEY (config_key)
		)`,
		`CREATE TABLE IF NOT EXISTS service_account_tokens (
			token_id VARCHAR(255) NOT NULL,
			token_name VARCHAR(255),
			token_hash VARCHAR(255) NOT NULL UNIQUE,
			created_at BIGINT NOT NULL,
			last_used_at BIGINT NOT NULL DEFAULT 0,
			expires_at BIGINT NOT NULL DEFAULT 0,
			creator VARCHAR(255),
			PRIMARY KEY (token_id)
		)`,
	}
	for _, q := range schemas {
		if _, err := b.db.Exec(q); err != nil {
			return err
		}
	}
	return b.migrateLegacyTable()
}

// migrateLegacyTable moves rows from the old single csp_state(kind,name,data)
// table into the per-entity tables, then drops it. No-op when absent.
func (b *sqlBackend) migrateLegacyTable() error {
	rows, err := b.db.Query(`SELECT kind, name, data, created_at, updated_at FROM csp_state`)
	if err != nil {
		return nil // legacy table absent; nothing to migrate
	}
	defer rows.Close()

	type legacy struct {
		name      string
		data      string
		createdAt int64
		updatedAt int64
	}
	byKind := map[string][]legacy{}
	for rows.Next() {
		var kind, name, data string
		var created, updated int64
		if err := rows.Scan(&kind, &name, &data, &created, &updated); err != nil {
			return err
		}
		byKind[kind] = append(byKind[kind], legacy{name: name, data: data, createdAt: created, updatedAt: updated})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for kind, items := range byKind {
		for _, it := range items {
			var args []any
			var q string
			switch kind {
			case "clusters":
				q = `INSERT INTO clusters (name, status, backend, cloud, region, num_nodes, data, created_at, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
				args = []any{it.name, "", "", "", "", 0, it.data, it.createdAt, it.updatedAt}
			case "services":
				q = `INSERT INTO services (name, status, replicas, port, data, created_at, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, ?)`
				args = []any{it.name, "", 0, 0, it.data, it.createdAt, it.updatedAt}
			case "jobs":
				q = `INSERT INTO jobs (name, status, schedule, run_count, fail_count, next_run, data, created_at, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
				args = []any{it.name, "", "", 0, 0, 0, it.data, it.createdAt, it.updatedAt}
			default:
				continue
			}
			if _, err := tx.Exec(q, args...); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if _, err := b.db.Exec(`DROP TABLE csp_state`); err != nil {
		return err
	}
	return nil
}

func (b *sqlBackend) LoadClusters() (map[string]*Cluster, error) {
	return loadSQL[*Cluster](b, clustersTable)
}

func (b *sqlBackend) LoadServices() (map[string]*Service, error) {
	return loadSQL[*Service](b, servicesTable)
}

func (b *sqlBackend) LoadJobs() (map[string]*Job, error) {
	return loadSQL[*Job](b, jobsTable)
}

func (b *sqlBackend) SaveClusters(clusters map[string]*Cluster) error {
	return saveSQL(b, clustersTable, clusters)
}

func (b *sqlBackend) SaveServices(services map[string]*Service) error {
	return saveSQL(b, servicesTable, services)
}

func (b *sqlBackend) SaveJobs(jobs map[string]*Job) error {
	return saveSQL(b, jobsTable, jobs)
}

func (b *sqlBackend) LoadClusterYAMLs() (map[string]*ClusterYAML, error) {
	q := fmt.Sprintf("SELECT cluster_name, yaml, created_at, updated_at FROM %s", clusterYAMLTable)
	rows, err := b.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*ClusterYAML{}
	for rows.Next() {
		var y ClusterYAML
		if err := rows.Scan(&y.ClusterName, &y.YAML, &y.CreatedAt, &y.UpdatedAt); err != nil {
			return nil, err
		}
		out[y.ClusterName] = &y
	}
	return out, rows.Err()
}

func (b *sqlBackend) SaveClusterYAMLs(yamls map[string]*ClusterYAML) error {
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	del := fmt.Sprintf("DELETE FROM %s", clusterYAMLTable)
	if _, err := tx.Exec(del); err != nil {
		return err
	}
	ins := fmt.Sprintf("INSERT INTO %s (cluster_name, yaml, created_at, updated_at) VALUES (?, ?, ?, ?)", clusterYAMLTable)
	for _, y := range yamls {
		if _, err := tx.Exec(ins, y.ClusterName, y.YAML, y.CreatedAt, y.UpdatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (b *sqlBackend) LoadClusterHistory() (map[string]*ClusterHistory, error) {
	q := fmt.Sprintf("SELECT cluster_name, num_nodes, cloud, region, zone, instance_type, backend, task_yaml, launched_at, created_at, updated_at FROM %s", clusterHistoryTable)
	rows, err := b.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*ClusterHistory{}
	for rows.Next() {
		var h ClusterHistory
		if err := rows.Scan(&h.ClusterName, &h.NumNodes, &h.Cloud, &h.Region, &h.Zone,
			&h.InstanceType, &h.Backend, &h.TaskYAML, &h.LaunchedAt, &h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, err
		}
		out[h.ClusterName] = &h
	}
	return out, rows.Err()
}

func (b *sqlBackend) SaveClusterHistory(history map[string]*ClusterHistory) error {
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	del := fmt.Sprintf("DELETE FROM %s", clusterHistoryTable)
	if _, err := tx.Exec(del); err != nil {
		return err
	}
	ins := fmt.Sprintf("INSERT INTO %s (cluster_name, num_nodes, cloud, region, zone, instance_type, backend, task_yaml, launched_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", clusterHistoryTable)
	for _, h := range history {
		if _, err := tx.Exec(ins, h.ClusterName, h.NumNodes, h.Cloud, h.Region, h.Zone,
			h.InstanceType, h.Backend, h.TaskYAML, h.LaunchedAt, h.CreatedAt, h.UpdatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (b *sqlBackend) LoadClusterEvents() ([]*ClusterEvent, error) {
	q := fmt.Sprintf("SELECT cluster_name, starting_status, ending_status, reason, type, request_id, transitioned_at FROM %s ORDER BY transitioned_at", clusterEventsTable)
	rows, err := b.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ClusterEvent
	for rows.Next() {
		var e ClusterEvent
		if err := rows.Scan(&e.ClusterName, &e.StartingStatus, &e.EndingStatus, &e.Reason, &e.Type, &e.RequestID, &e.TransitionedAt); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

func (b *sqlBackend) SaveClusterEvents(events []*ClusterEvent) error {
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	del := fmt.Sprintf("DELETE FROM %s", clusterEventsTable)
	if _, err := tx.Exec(del); err != nil {
		return err
	}
	ins := fmt.Sprintf("INSERT INTO %s (cluster_name, starting_status, ending_status, reason, type, request_id, transitioned_at) VALUES (?, ?, ?, ?, ?, ?, ?)", clusterEventsTable)
	for _, e := range events {
		if _, err := tx.Exec(ins, e.ClusterName, e.StartingStatus, e.EndingStatus, e.Reason, e.Type, e.RequestID, e.TransitionedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (b *sqlBackend) Close() error { return b.db.Close() }

func (b *sqlBackend) LoadConfig() (map[string]*ConfigEntry, error) {
	q := fmt.Sprintf("SELECT config_key, config_value, created_at, updated_at FROM %s", configTable)
	rows, err := b.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*ConfigEntry{}
	for rows.Next() {
		var c ConfigEntry
		if err := rows.Scan(&c.Key, &c.Value, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out[c.Key] = &c
	}
	return out, rows.Err()
}

func (b *sqlBackend) SaveConfig(entries map[string]*ConfigEntry) error {
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	del := fmt.Sprintf("DELETE FROM %s", configTable)
	if _, err := tx.Exec(del); err != nil {
		return err
	}
	ins := fmt.Sprintf("INSERT INTO %s (config_key, config_value, created_at, updated_at) VALUES (?, ?, ?, ?)", configTable)
	for _, c := range entries {
		if _, err := tx.Exec(ins, c.Key, c.Value, c.CreatedAt, c.UpdatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (b *sqlBackend) LoadTokens() ([]*ServiceAccountToken, error) {
	q := fmt.Sprintf("SELECT token_id, token_name, token_hash, created_at, last_used_at, expires_at, creator FROM %s ORDER BY created_at", tokensTable)
	rows, err := b.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ServiceAccountToken
	for rows.Next() {
		var t ServiceAccountToken
		if err := rows.Scan(&t.TokenID, &t.TokenName, &t.TokenHash, &t.CreatedAt, &t.LastUsedAt, &t.ExpiresAt, &t.Creator); err != nil {
			return nil, err
		}
		t.Active = !t.Expired(nowUnix())
		out = append(out, &t)
	}
	return out, rows.Err()
}

func (b *sqlBackend) SaveTokens(tokens []*ServiceAccountToken) error {
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	del := fmt.Sprintf("DELETE FROM %s", tokensTable)
	if _, err := tx.Exec(del); err != nil {
		return err
	}
	ins := fmt.Sprintf("INSERT INTO %s (token_id, token_name, token_hash, created_at, last_used_at, expires_at, creator) VALUES (?, ?, ?, ?, ?, ?, ?)", tokensTable)
	for _, t := range tokens {
		if _, err := tx.Exec(ins, t.TokenID, t.TokenName, t.TokenHash, t.CreatedAt, t.LastUsedAt, t.ExpiresAt, t.Creator); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func loadSQL[T any](b *sqlBackend, table string) (map[string]T, error) {
	q := fmt.Sprintf("SELECT name, data FROM %s", table)
	rows, err := b.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]T{}
	for rows.Next() {
		var name string
		var raw string
		if err := rows.Scan(&name, &raw); err != nil {
			return nil, err
		}
		var v T
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		out[name] = v
	}
	return out, rows.Err()
}

// saveSQL replaces all rows of a table in a single transaction, keeping the
// store semantics identical to the file backend (full-state save per mutation).
// Indexed columns are filled from each entity so they can be queried directly.
func saveSQL[T any](b *sqlBackend, table string, items map[string]T) error {
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	del := fmt.Sprintf("DELETE FROM %s", table)
	if _, err := tx.Exec(del); err != nil {
		return err
	}

	cols := columnsFor(table)
	ins := fmt.Sprintf("INSERT INTO %s (name, %s, data, created_at, updated_at) VALUES (?, %s, ?, ?, ?)",
		table, strings.Join(cols, ", "), placeholders(len(cols)))
	now := time.Now().Unix()
	for name, item := range items {
		data, err := json.Marshal(item)
		if err != nil {
			return err
		}
		created, updated := now, now
		if v, ok := any(item).(timestamps); ok {
			if t := v.createdAt(); t > 0 {
				created = t
			}
			if t := v.updatedAt(); t > 0 {
				updated = t
			}
		}
		args := append([]any{name}, indexedValues(table, item)...)
		args = append(args, string(data), created, updated)
		if _, err := tx.Exec(ins, args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// columnsFor returns the indexed column names for a table.
func columnsFor(table string) []string {
	switch table {
	case clustersTable:
		return []string{"status", "backend", "cloud", "region", "num_nodes", "task_yaml"}
	case servicesTable:
		return []string{"status", "replicas", "port"}
	case jobsTable:
		return []string{"status", "schedule", "run_count", "fail_count", "next_run"}
	}
	return nil
}

// indexedValues extracts the indexed column values from an entity.
func indexedValues(table string, item any) []any {
	switch table {
	case clustersTable:
		c := item.(*Cluster)
		return []any{c.Status, c.Backend, c.Cloud, c.Region, c.NumNodes, c.TaskYAML}
	case servicesTable:
		s := item.(*Service)
		return []any{s.Status, s.Replicas, s.Port}
	case jobsTable:
		j := item.(*Job)
		return []any{j.Status, j.Schedule, j.RunCount, j.FailCount, j.NextRun}
	}
	return nil
}

func placeholders(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}

// timestamps exposes the CreatedAt/UpdatedAt fields used by state entities so
// the SQL backend can preserve them across full-state saves.
type timestamps interface {
	createdAt() int64
	updatedAt() int64
}
