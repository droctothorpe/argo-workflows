package db

import (
	"context"
	"encoding/json"
	"errors"
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

func NewQueries(tableName string, dbType sqldb.DBType) *Queries {
	return &Queries{tableName: tableName, dbType: dbType}
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
		err := sess.Collection(q.tableName).
			Find(db.Cond{
				"namespace":  namespace,
				"cache_name": cacheName,
				"key":        key,
			}).
			One(&r)
		if errors.Is(err, db.ErrNoMoreRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		return nil
	})
	if err != nil || !found {
		return nil, err
	}

	// Only update last_hit_at if the existing value is stale, to avoid a write on every read.
	logger := logging.RequireLoggerFromContext(ctx)
	now := time.Now()
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
	now := time.Now()
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
