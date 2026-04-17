package db

import (
	"context"
	"fmt"
	"regexp"

	"github.com/upper/db/v4"

	"github.com/argoproj/argo-workflows/v4/util/sqldb"
)

var validTableName = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func migrate(ctx context.Context, session db.Session, dbType sqldb.DBType, tableName string) error {
	if !validTableName.MatchString(tableName) {
		return fmt.Errorf("invalid table name %q: must match [A-Za-z0-9_]+", tableName)
	}
	return sqldb.Migrate(ctx, session, dbType, versionTable, []sqldb.Change{
		// MySQL: use backticks around `key` (reserved word) and LONGTEXT for outputs (TEXT is 64KB).
		// Postgres: use double-quotes around "key" and text (no size limit).
		sqldb.ByType(dbType, sqldb.TypedChanges{
			sqldb.Postgres: sqldb.AnsiSQLChange(`create table if not exists ` + tableName + ` (
    cache_name  varchar(256) not null,
    "key"       varchar(256) not null,
    node_id     text         not null,
    outputs     text         not null,
    created_at  timestamp    not null,
    last_hit_at timestamp    not null,
    primary key (cache_name, "key")
)`),
			sqldb.MySQL: sqldb.AnsiSQLChange("create table if not exists " + tableName + " (" +
				"cache_name  varchar(256) not null, " +
				"`key`       varchar(256) not null, " +
				"node_id     text         not null, " +
				"outputs     longtext     not null, " +
				"created_at  timestamp    not null, " +
				"last_hit_at timestamp    not null, " +
				"primary key (cache_name, `key`))"),
		}),
		sqldb.AnsiSQLChange(`create index imemo_last_hit_at on ` + tableName + ` (last_hit_at)`),
	})
}
