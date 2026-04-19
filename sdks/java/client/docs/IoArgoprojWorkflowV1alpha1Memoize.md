

# IoArgoprojWorkflowV1alpha1Memoize

Memoize enables caching for the Outputs of the template.

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**cache** | [**IoArgoprojWorkflowV1alpha1Cache**](IoArgoprojWorkflowV1alpha1Cache.md) |  |  [optional]
**key** | **String** | Key is the key to use as the caching key. If not set, a deterministic key is derived from the template name and all resolved input parameters and artifacts. |  [optional]
**maxAge** | **String** | MaxAge is the maximum age (e.g. \&quot;180s\&quot;, \&quot;24h\&quot;) of an entry that is still considered valid. If an entry is older than the MaxAge, it will be ignored. When omitted, entries are valid until removed by the GC (see CacheTTL). |  [optional]



