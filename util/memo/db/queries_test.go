package db_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	testcontainers "github.com/testcontainers/testcontainers-go"
	testpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/argoproj/argo-workflows/v4/config"
	wfv1 "github.com/argoproj/argo-workflows/v4/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v4/util/logging"
	memodb "github.com/argoproj/argo-workflows/v4/util/memo/db"
	"github.com/argoproj/argo-workflows/v4/util/sqldb"
)

const (
	testDBName     = "memotest"
	testDBUser     = "user"
	testDBPassword = "pass"
	testTableName  = "memoization_cache"
	testNamespace  = "default"
	testCacheName  = "my-cache"
)

// setupPostgres starts a throwaway Postgres container and returns a migrated SessionProxy.
func setupPostgres(ctx context.Context, t *testing.T) *sqldb.SessionProxy {
	t.Helper()
	pg, err := testpostgres.Run(ctx,
		"postgres:17.4-alpine",
		testpostgres.WithDatabase(testDBName),
		testpostgres.WithUsername(testDBUser),
		testpostgres.WithPassword(testDBPassword),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		if termErr := testcontainers.TerminateContainer(pg); termErr != nil {
			t.Logf("failed to terminate postgres container: %s", termErr)
		}
	})

	host, err := pg.Host(ctx)
	require.NoError(t, err)
	portStr, err := pg.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr.Port())
	require.NoError(t, err)

	dbCfg := config.DBConfig{
		PostgreSQL: &config.PostgreSQLConfig{
			DatabaseConfig: config.DatabaseConfig{
				Host:     host,
				Port:     port,
				Database: testDBName,
			},
		},
	}
	sp, err := sqldb.NewSessionProxy(ctx, sqldb.SessionProxyConfig{
		DBConfig:   dbCfg,
		Username:   testDBUser,
		Password:   testDBPassword,
		MaxRetries: 5,
		BaseDelay:  200 * time.Millisecond,
		MaxDelay:   10 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sp.Close() })

	memoCfg := &config.MemoizationConfig{
		DBConfig:  dbCfg,
		TableName: testTableName,
	}
	memodb.Migrate(ctx, sp, memodb.ConfigFromConfig(memoCfg))
	return sp
}

func sampleOutputs(message string) *wfv1.Outputs {
	return &wfv1.Outputs{
		Parameters: []wfv1.Parameter{
			{Name: "result", Value: wfv1.AnyStringPtr(message)},
		},
	}
}

func TestQueriesSaveAndLoad(t *testing.T) {
	ctx := logging.TestContext(t.Context())
	sp := setupPostgres(ctx, t)
	q := memodb.NewQueries(testTableName)

	// Load returns nil when no entry exists.
	rec, err := q.Load(ctx, sp, testNamespace, testCacheName, "key1")
	require.NoError(t, err)
	assert.Nil(t, rec, "expected nil for missing key")

	// Save an entry and load it back.
	require.NoError(t, q.Save(ctx, sp, testNamespace, testCacheName, "key1", "node-abc", sampleOutputs("hello")))
	rec, err = q.Load(ctx, sp, testNamespace, testCacheName, "key1")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "node-abc", rec.NodeID)
	assert.Contains(t, rec.Outputs, "hello")
}

func TestQueriesLoadUpdatesLastHitAt(t *testing.T) {
	ctx := logging.TestContext(t.Context())
	sp := setupPostgres(ctx, t)
	q := memodb.NewQueries(testTableName)

	require.NoError(t, q.Save(ctx, sp, testNamespace, testCacheName, "key2", "node-1", sampleOutputs("v1")))

	first, err := q.Load(ctx, sp, testNamespace, testCacheName, "key2")
	require.NoError(t, err)
	require.NotNil(t, first)

	// Wait a moment so the second load gets a later timestamp.
	time.Sleep(5 * time.Millisecond)

	second, err := q.Load(ctx, sp, testNamespace, testCacheName, "key2")
	require.NoError(t, err)
	require.NotNil(t, second)

	assert.True(t, second.LastHitAt.After(first.LastHitAt),
		"last_hit_at should advance on each Load (first=%v second=%v)", first.LastHitAt, second.LastHitAt)
}

func TestQueriesSaveReplaces(t *testing.T) {
	ctx := logging.TestContext(t.Context())
	sp := setupPostgres(ctx, t)
	q := memodb.NewQueries(testTableName)

	require.NoError(t, q.Save(ctx, sp, testNamespace, testCacheName, "key3", "node-old", sampleOutputs("old")))
	require.NoError(t, q.Save(ctx, sp, testNamespace, testCacheName, "key3", "node-new", sampleOutputs("new")))

	rec, err := q.Load(ctx, sp, testNamespace, testCacheName, "key3")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "node-new", rec.NodeID)
	assert.Contains(t, rec.Outputs, "new")
}

func TestQueriesPruneRemovesOldEntries(t *testing.T) {
	ctx := logging.TestContext(t.Context())
	sp := setupPostgres(ctx, t)
	q := memodb.NewQueries(testTableName)

	// Save two entries.
	require.NoError(t, q.Save(ctx, sp, testNamespace, testCacheName, "old-key", "node-old", sampleOutputs("old")))
	require.NoError(t, q.Save(ctx, sp, testNamespace, testCacheName, "new-key", "node-new", sampleOutputs("new")))

	// Backdate old-key's last_hit_at to 10 days ago.
	_, err := sp.Session().SQL().
		Exec("UPDATE "+testTableName+" SET last_hit_at = ? WHERE key = 'old-key'", time.Now().Add(-10*24*time.Hour))
	require.NoError(t, err)

	// Prune with 5-day TTL — old-key should be deleted, new-key should survive.
	n, err := q.Prune(ctx, sp, 5*24*time.Hour)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "expected exactly one row pruned")

	old, err := q.Load(ctx, sp, testNamespace, testCacheName, "old-key")
	require.NoError(t, err)
	assert.Nil(t, old, "old entry should have been pruned")

	fresh, err := q.Load(ctx, sp, testNamespace, testCacheName, "new-key")
	require.NoError(t, err)
	assert.NotNil(t, fresh, "new entry should still exist")
}

func TestQueriesPruneKeepsRecentEntries(t *testing.T) {
	ctx := logging.TestContext(t.Context())
	sp := setupPostgres(ctx, t)
	q := memodb.NewQueries(testTableName)

	require.NoError(t, q.Save(ctx, sp, testNamespace, testCacheName, "recent", "node-1", sampleOutputs("v1")))

	// All entries are recent — nothing should be pruned.
	n, err := q.Prune(ctx, sp, 90*24*time.Hour)
	require.NoError(t, err)
	assert.EqualValues(t, 0, n, "expected no rows pruned when all entries are fresh")
}
