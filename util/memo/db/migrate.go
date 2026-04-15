package db

import (
	"context"

	"github.com/upper/db/v4"

	"github.com/argoproj/argo-workflows/v4/util/sqldb"
)

func migrate(ctx context.Context, session db.Session, dbType sqldb.DBType, tableName string) error {
	return sqldb.Migrate(ctx, session, dbType, versionTable, []sqldb.Change{
		sqldb.AnsiSQLChange(`create table if not exists ` + tableName + ` (
    namespace  varchar(256) not null,
    cache_name varchar(256) not null,
    key        varchar(256) not null,
    node_id    text         not null,
    outputs    text         not null,
    created_at timestamp    not null,
    last_hit_at timestamp   not null,
    primary key (namespace, cache_name, key)
)`),
		sqldb.AnsiSQLChange(`create index if not exists imemo_namespace_cache on ` + tableName + ` (namespace, cache_name)`),
	})
}
