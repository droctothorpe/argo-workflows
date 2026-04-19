package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/upper/db/v4"

	wfv1 "github.com/argoproj/argo-workflows/v4/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v4/util/logging"
	"github.com/argoproj/argo-workflows/v4/util/sqldb"
)

// CacheRecord is the database row for a single memoization cache entry.
type CacheRecord struct {
	Namespace string    `db:"namespace"`
	CacheName string    `db:"cache_name"`
	Key       string    `db:"key"`
	NodeID    string    `db:"node_id"`
	Outputs   string    `db:"outputs"` // JSON
	CreatedAt time.Time `db:"created_at"`
	LastHitAt time.Time `db:"last_hit_at"`
}

// Queries provides database operations for the memoization cache table.
type Queries struct {
	tableName string
	dbType    sqldb.DBType
}

func NewQueries(tableName string, dbType sqldb.DBType) (*Queries, error) {
	if !validTableName.MatchString(tableName) {
		return nil, fmt.Errorf("invalid table name %q: must match [A-Za-z0-9_]+", tableName)
	}
	return &Queries{tableName: tableName, dbType: dbType}, nil
}

// lastHitAtUpdateInterval controls how often last_hit_at is refreshed on cache reads. Updates are
// skipped when the existing value is newer than this threshold, avoiding a write transaction on
// every Load while still keeping GC TTL accurate (default TTL is 90 days).
const lastHitAtUpdateInterval = 1 * time.Hour

// Load retrieves the outputs for the given key and refreshes last_hit_at if it is stale.
// Returns nil when the entry does not exist.
func (q *Queries) Load(ctx context.Context, sp *sqldb.SessionProxy, namespace, cacheName, key string) (*CacheRecord, error) {
	var r CacheRecord
	var found bool
	err := sp.With(ctx, func(sess db.Session) error {
		// Use raw SQL to avoid upper/db ORM timestamp scanning issues with
		// "timestamp without timezone" columns (the ORM may not populate time.Time fields).
		var query string
		switch q.dbType {
		case sqldb.Postgres:
			query = fmt.Sprintf(`SELECT namespace, cache_name, "key", node_id, outputs, created_at, last_hit_at FROM %s WHERE namespace = $1 AND cache_name = $2 AND "key" = $3`, q.tableName)
		case sqldb.MySQL:
			query = fmt.Sprintf("SELECT namespace, cache_name, `key`, node_id, outputs, created_at, last_hit_at FROM %s WHERE namespace = ? AND cache_name = ? AND `key` = ?", q.tableName)
		default:
			return fmt.Errorf("unsupported database type: %s", q.dbType)
		}
		rows, err := sess.SQL().QueryContext(ctx, query, namespace, cacheName, key)
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			return rows.Err()
		}
		found = true
		return rows.Scan(&r.Namespace, &r.CacheName, &r.Key, &r.NodeID, &r.Outputs, &r.CreatedAt, &r.LastHitAt)
	})
	if err != nil || !found {
		return nil, err
	}

	// Only update last_hit_at if the existing value is stale, to avoid a write on every read.
	logger := logging.RequireLoggerFromContext(ctx)
	now := time.Now().UTC()
	if now.Sub(r.LastHitAt) >= lastHitAtUpdateInterval {
		if err := sp.With(ctx, func(sess db.Session) error {
			_, err := sess.SQL().
				Update(q.tableName).
				Set("last_hit_at", now).
				Where(db.Cond{
					"namespace":  namespace,
					"cache_name": cacheName,
					"key":        key,
				}).
				Exec()
			return err
		}); err != nil {
			logger.WithError(err).Warn(ctx, "Failed to update last_hit_at for memoization cache entry")
		} else {
			r.LastHitAt = now
		}
	}

	return &r, nil
}

// Prune deletes cache entries whose last_hit_at is older than maxAge. It is called periodically
// by the controller to bound the size of the memoization_cache table.
func (q *Queries) Prune(ctx context.Context, sp *sqldb.SessionProxy, maxAge time.Duration) (int64, error) {
	cutoff := time.Now().Add(-maxAge)
	var n int64
	err := sp.With(ctx, func(sess db.Session) error {
		result, err := sess.SQL().
			DeleteFrom(q.tableName).
			Where("last_hit_at < ?", cutoff).
			Exec()
		if err != nil {
			return err
		}
		n, err = result.RowsAffected()
		return err
	})
	return n, err
}

func (q *Queries) Save(ctx context.Context, sp *sqldb.SessionProxy, namespace, cacheName, key, nodeID string, outputs *wfv1.Outputs) error {
	outputsJSON, err := json.Marshal(outputs)
	if err != nil {
		return fmt.Errorf("unable to marshal memoization outputs: %w", err)
	}
	outputsStr := string(outputsJSON)
	now := time.Now().UTC()
	return sp.With(ctx, func(sess db.Session) error {
		switch q.dbType {
		case sqldb.Postgres:
			_, err := sess.SQL().ExecContext(ctx,
				fmt.Sprintf(`INSERT INTO %s (namespace, cache_name, "key", node_id, outputs, created_at, last_hit_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (namespace, cache_name, "key") DO UPDATE SET node_id = $4, outputs = $5, last_hit_at = $7`, q.tableName),
				namespace, cacheName, key, nodeID, outputsStr, now, now)
			return err
		case sqldb.MySQL:
			_, err := sess.SQL().ExecContext(ctx,
				fmt.Sprintf("INSERT INTO %s (namespace, cache_name, `key`, node_id, outputs, created_at, last_hit_at) VALUES (?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE node_id = ?, outputs = ?, last_hit_at = ?", q.tableName),
				namespace, cacheName, key, nodeID, outputsStr, now, now, nodeID, outputsStr, now)
			return err
		default:
			return fmt.Errorf("unsupported database type: %s", q.dbType)
		}
	})
}
