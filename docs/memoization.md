# Step Level Memoization

> v2.10 and after

## Introduction

Workflows often have outputs that are expensive to compute.
Memoization reduces cost and workflow execution time by recording the result of previously run steps:
it stores the outputs of a template and replays them on subsequent runs when the same inputs are seen.

Prior to version 3.5 memoization only works for steps which have outputs, if you attempt to use it on steps which do not it should not work (there are some cases where it does, but they shouldn't). It was designed for 'pure' steps, where the purpose of running the step is to calculate some outputs based upon the step's inputs, and only the inputs. Pure steps should not interact with the outside world, but workflows won't enforce this on you.

If you are using workflows prior to version 3.5 you should look at the [work avoidance](work-avoidance.md) technique instead of memoization if your steps don't have outputs.

In version 3.5 or later all steps can be memoized, whether or not they have outputs.

## Cache Backends

Argo Workflows supports two backends for storing memoization cache entries:

### ConfigMap (default)

By default, cached data is stored in Kubernetes ConfigMaps.
This allows you to easily manipulate cache entries manually through `kubectl` and the Kubernetes API without having to go through Argo.
All cache ConfigMaps must have the label `workflows.argoproj.io/configmap-type: Cache` to be used as a cache. This prevents accidental access to other important ConfigMaps in the system.

### SQL Database

> v4.0 and after

Alternatively, cache entries can be stored in a PostgreSQL or MySQL database. This is recommended for production use — it has no size limits, supports long-term persistence, and includes automatic garbage collection.

To enable SQL-backed memoization, add a `memoization` block to the `workflow-controller-configmap`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: workflow-controller-configmap
  namespace: argo
data:
  memoization: |
    tableName: memoization_cache
    postgresql:
      host: postgres
      port: 5432
      database: postgres
      userNameSecret:
        name: argo-postgres-config
        key: username
      passwordSecret:
        name: argo-postgres-config
        key: password
    cacheTTL: "2160h"
```

The `cacheTTL` field controls how long cache entries that have not been accessed are retained by the garbage collector.
It defaults to `"2160h"` (90 days). Set to a negative value (e.g., `"-1s"`) to disable automatic pruning.

MySQL is also supported:

```yaml
  memoization: |
    tableName: memoization_cache
    mysql:
      host: mysql
      port: 3306
      database: argo
      userNameSecret:
        name: argo-mysql-config
        key: username
      passwordSecret:
        name: argo-mysql-config
        key: password
```

## Using Memoization

Memoization is configured at the template level via the `memoize` field.

### Minimal form (SQL backend)

When SQL memoization is configured on the controller, the simplest valid spec requires no additional fields:

```yaml
templates:
  - name: my-template
    memoize: {}
    # ...
```

Cache entries are stored under a `"default"` logical bucket in the database.

### Full form

```yaml
templates:
  - name: print-message
    memoize:
      key: "{{inputs.parameters.message}}"
      maxAge: "24h"
      cache:
        configMap:
          name: print-message-cache
    # ...
```

### Fields

| Field | Required | Description |
|-------|----------|-------------|
| `key` | No | The cache lookup key. When omitted, a deterministic key is automatically computed from the template name and all resolved input parameters and artifact checksums. |
| `cache` | Conditional | Specifies the cache storage. Required when using the ConfigMap backend. When omitted with the SQL backend, entries are stored in the `"default"` bucket. |
| `maxAge` | No | Maximum age of a cache entry (e.g. `"180s"`, `"24h"`). Entries older than this are treated as misses at lookup time. When omitted, entries are valid until removed by the GC (`cacheTTL`). Only relevant for time-sensitive computations. |

### Automatic cache key derivation

When `key` is not set, Argo automatically derives a deterministic key from:

- The template name
- All resolved input **parameters** (name and value, length-prefixed)
- All resolved input **artifact checksums** (SHA-256 for executor-computed artifacts, ETag for driver-provided artifacts)

The manifest is hashed with SHA-256, producing a 64-character hex key. This means identical inputs will always produce the same key, and any change to an input (including artifact content) will produce a different key.

You can still provide an explicit `key` if you need custom cache partitioning (e.g. per-user or per-environment caches).

### Cache groups (SQL backend)

When using the SQL backend, `cache.configMap.name` acts as a logical group name in the database — no ConfigMap is created. This lets you partition cache entries by workflow, team, or environment:

```yaml
memoize:
  cache:
    configMap:
      name: my-team-cache
```

When `cache` is omitted entirely, entries are stored under the `"default"` group.

[Find a simple example for memoization here](https://github.com/argoproj/argo-workflows/blob/main/examples/memoize-simple.yaml).

!!! Note
    To use memoization with the ConfigMap backend, add the verbs `create` and `update` to the `configmaps` resource for the appropriate (cluster) roles. For a cluster install, update the `argo-cluster-role` cluster role; for a namespace install, update the `argo-role` role. This is not required when using the SQL database backend.

## FAQ

1. If you see errors like `error creating cache entry: ConfigMap \"reuse-task\" is invalid: []: Too long: must have at most 1048576 characters`,
   this is due to [the 1MB limit placed on the size of `ConfigMap`](https://github.com/kubernetes/kubernetes/issues/19781).
   Here are a couple of ways that might help resolve this:
    - Delete the existing `ConfigMap` cache or switch to use a different cache.
    - Reduce the size of the output parameters for the nodes that are being memoized.
    - Split your cache into different memoization keys and cache names so that each cache entry is small.
    - Switch to the SQL database backend which has no size limit.
1. My step isn't getting memoized, why not?
   If you are running workflows <3.5 ensure that you have specified at least one output on the step.
