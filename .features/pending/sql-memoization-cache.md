Description: Add SQL database-backed memoization cache as an alternative to ConfigMaps.
Authors: [droctothorpe](https://github.com/droctothorpe)
Component: General
Issues: 15952
PRs: 15938

Memoization can now store cache entries in a PostgreSQL or MySQL database instead of Kubernetes ConfigMaps.
The SQL backend removes the 1 MB ConfigMap size limit and persists cache entries across cluster restarts.
ConfigMaps remain the default; opt in by adding a `memoization` block to the `workflow-controller-configmap`.
A `cacheTTL` field controls how long unaccessed entries are retained (default: `2160h` / 90 days).
No changes to workflow specs are required.
