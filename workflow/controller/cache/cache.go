package cache

import (
	"context"
	"log"
	"regexp"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	wfv1 "github.com/argoproj/argo-workflows/v4/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v4/util/sqldb"
)

var cacheKeyRegex = regexp.MustCompile("^[a-zA-Z0-9][-a-zA-Z0-9]*$")

type MemoizationCache interface {
	Load(ctx context.Context, key string) (*Entry, error)
	Save(ctx context.Context, key string, nodeID string, value *wfv1.Outputs) error
}

type Entry struct {
	NodeID            string        `json:"nodeID"`
	Outputs           *wfv1.Outputs `json:"outputs"`
	CreationTimestamp metav1.Time   `json:"creationTimestamp"`
	LastHitTimestamp  metav1.Time   `json:"lastHitTimestamp"`
}

func (e *Entry) Hit() bool {
	return e != nil && e.NodeID != ""
}

func (e *Entry) GetOutputs() *wfv1.Outputs {
	if e == nil {
		return nil
	}
	return e.Outputs
}

func (e *Entry) GetOutputsWithMaxAge(maxAge time.Duration) (*wfv1.Outputs, bool) {
	if e == nil {
		return nil, false
	}
	if time.Since(e.CreationTimestamp.Time) > maxAge {
		// Outputs have expired
		return nil, false
	}
	return e.Outputs, true
}

type cacheFactory struct {
	caches     map[string]MemoizationCache
	kubeclient kubernetes.Interface
	// namespace is the controller's install namespace, used both for ConfigMap operations and as
	// the namespace column in the SQL backend. All cache entries share this value.
	namespace    string
	lock         sync.RWMutex
	sessionProxy *sqldb.SessionProxy
	tableName    string
	// sqlEnabled indicates that SQL caching was explicitly configured by the operator, even if
	// the session proxy is currently unavailable. When true and sessionProxy is nil, GetCache
	// returns nil rather than silently falling back to ConfigMap-based caching.
	sqlEnabled bool
}

type Factory interface {
	GetCache(ct Type, name string) MemoizationCache
	// SetSessionProxy configures the factory's SQL backend. sqlEnabled indicates whether SQL
	// caching is currently configured; when true with a nil sp (e.g. after a DB failure), GetCache
	// returns nil rather than silently falling back to ConfigMap-based caching. Calling this clears
	// any previously created cache instances so they are recreated against the new backend.
	SetSessionProxy(sp *sqldb.SessionProxy, tableName string, sqlEnabled bool)
}

func NewCacheFactory(ki kubernetes.Interface, ns string) Factory {
	return &cacheFactory{
		caches:     make(map[string]MemoizationCache),
		kubeclient: ki,
		namespace:  ns,
	}
}

type Type string

const (
	// ConfigMapCache is a cache type identifier used as a key prefix in the cache map.
	// When a database session proxy is configured, SQL-backed caching is used instead.
	ConfigMapCache Type = "ConfigMapCache"
)

// SetSessionProxy configures the factory's SQL backend, clearing any previously cached instances.
// sqlEnabled should be true whenever SQL memoization is configured by the operator, including when
// the session proxy is unavailable (e.g. after a DB failure). When sqlEnabled is true but sp is
// nil, GetCache returns nil rather than silently falling back to ConfigMap-based caching.
func (cf *cacheFactory) SetSessionProxy(sp *sqldb.SessionProxy, tableName string, sqlEnabled bool) {
	cf.lock.Lock()
	defer cf.lock.Unlock()
	cf.sessionProxy = sp
	cf.tableName = tableName
	cf.sqlEnabled = sqlEnabled
	cf.caches = make(map[string]MemoizationCache)
}

// Returns a cache if it exists and creates it otherwise
func (cf *cacheFactory) GetCache(ct Type, name string) MemoizationCache {
	cf.lock.RLock()

	idx := string(ct) + "." + name
	if c := cf.caches[idx]; c != nil {
		cf.lock.RUnlock()
		return c
	}
	cf.lock.RUnlock()

	cf.lock.Lock()
	defer cf.lock.Unlock()

	if c := cf.caches[idx]; c != nil {
		return c
	}

	switch ct {
	case ConfigMapCache:
		var c MemoizationCache
		//nolint:gocritic
		if cf.sessionProxy != nil {
			var err error
			c, err = newSQLDBCache(cf.namespace, name, cf.sessionProxy, cf.tableName)
			if err != nil {
				log.Printf("failed to create SQL memoization cache %q: %v", name, err)
				return nil
			}
		} else if cf.sqlEnabled {
			// SQL was explicitly configured but is currently unavailable. Return nil so callers
			// can skip caching rather than silently redirecting to a ConfigMap backend.
			log.Printf("SQL memoization cache %q requested but SQL backend is unavailable; skipping cache", name)
			return nil
		} else {
			c = NewConfigMapCache(cf.namespace, cf.kubeclient, name)
		}
		cf.caches[idx] = c
		return c
	default:
		return nil
	}
}
