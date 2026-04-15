package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/upper/db/v4"

	wfv1 "github.com/argoproj/argo-workflows/v4/pkg/apis/workflow/v1alpha1"
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
}

func NewQueries(tableName string) *Queries {
	return &Queries{tableName: tableName}
}

// Load retrieves the outputs for the given key and updates last_hit_at. Returns nil when the entry
// does not exist.
func (q *Queries) Load(ctx context.Context, sp *sqldb.SessionProxy, namespace, cacheName, key string) (*CacheRecord, error) {
	var record *CacheRecord
	err := sp.TxWith(ctx, func(txProxy *sqldb.SessionProxy) error {
		sess := txProxy.Session()
		var r CacheRecord
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
		r.LastHitAt = time.Now()
		_, err = sess.SQL().
			Update(q.tableName).
			Set("last_hit_at", r.LastHitAt).
			Where(db.Cond{
				"namespace":  namespace,
				"cache_name": cacheName,
				"key":        key,
			}).
			Exec()
		if err != nil {
			return err
		}
		record = &r
		return nil
	}, nil)
	return record, err
}

// Prune deletes cache entries whose last_hit_at is older than maxAge. It is called periodically
// by the controller to bound the size of the memoization_cache table.
func (q *Queries) Prune(ctx context.Context, sp *sqldb.SessionProxy, maxAge time.Duration) (int64, error) {
	cutoff := time.Now().Add(-maxAge)
	result, err := sp.Session().SQL().
		DeleteFrom(q.tableName).
		Where("last_hit_at < ?", cutoff).
		Exec()
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	return n, err
}
func (q *Queries) Save(ctx context.Context, sp *sqldb.SessionProxy, namespace, cacheName, key, nodeID string, outputs *wfv1.Outputs) error {
	outputsJSON, err := json.Marshal(outputs)
	if err != nil {
		return fmt.Errorf("unable to marshal memoization outputs: %w", err)
	}
	now := time.Now()
	return sp.TxWith(ctx, func(txProxy *sqldb.SessionProxy) error {
		sess := txProxy.Session()
		_, err := sess.SQL().
			DeleteFrom(q.tableName).
			Where(db.Cond{
				"namespace":  namespace,
				"cache_name": cacheName,
				"key":        key,
			}).
			Exec()
		if err != nil {
			return err
		}
		_, err = sess.Collection(q.tableName).Insert(&CacheRecord{
			Namespace: namespace,
			CacheName: cacheName,
			Key:       key,
			NodeID:    nodeID,
			Outputs:   string(outputsJSON),
			CreatedAt: now,
			LastHitAt: now,
		})
		return err
	}, nil)
}
