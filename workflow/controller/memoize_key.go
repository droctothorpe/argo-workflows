package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	wfv1 "github.com/argoproj/argo-workflows/v4/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v4/util/logging"
	"github.com/argoproj/argo-workflows/v4/workflow/artifacts"
	artifactcommon "github.com/argoproj/argo-workflows/v4/workflow/artifacts/common"
)

// wocResourceInterface adapts the controller's kubeclientset to the resource.Interface
// expected by artifacts.NewDriver, scoped to the workflow's namespace.
type wocResourceInterface struct {
	woc *wfOperationCtx
}

func (r *wocResourceInterface) GetSecret(ctx context.Context, name, key string) (string, error) {
	secret, err := r.woc.controller.kubeclientset.CoreV1().Secrets(r.woc.wf.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get secret %s/%s: %w", r.woc.wf.Namespace, name, err)
	}
	val, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("secret %s/%s has no key %q", r.woc.wf.Namespace, name, key)
	}
	return string(val), nil
}

func (r *wocResourceInterface) GetConfigMapKey(ctx context.Context, name, key string) (string, error) {
	cm, err := r.woc.controller.kubeclientset.CoreV1().ConfigMaps(r.woc.wf.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get configmap %s/%s: %w", r.woc.wf.Namespace, name, err)
	}
	val, ok := cm.Data[key]
	if !ok {
		return "", fmt.Errorf("configmap %s/%s has no key %q", r.woc.wf.Namespace, name, key)
	}
	return val, nil
}

// memoizationKey returns the effective cache key for the given template.
//
// If memoize.key is set by the user it is returned as-is (explicit override).
// Otherwise a deterministic key is computed from:
//   - the template name (scope isolation across templates)
//   - all resolved input parameters, sorted by name
//   - all resolved input artifacts, resolved to a checksum or ETag where
//     possible, falling back to the artifact storage path
//
// Any artifact whose checksum cannot be determined is silently fallen back to
// path-identity. The key derivation is intentionally non-fatal: worst case a
// false cache miss is returned (never a false hit).
func memoizationKey(ctx context.Context, woc *wfOperationCtx, tmpl *wfv1.Template) (string, error) {
	if tmpl.Memoize.Key != "" {
		return tmpl.Memoize.Key, nil
	}

	ri := &wocResourceInterface{woc: woc}

	var lines []string

	// Template name scopes the key so two different templates with the same
	// inputs don't collide in the cache.
	lines = append(lines, "template:"+tmpl.Name)

	// Parameters — sorted for determinism.
	if tmpl.Inputs.Parameters != nil {
		params := make([]wfv1.Parameter, len(tmpl.Inputs.Parameters))
		copy(params, tmpl.Inputs.Parameters)
		sort.Slice(params, func(i, j int) bool { return params[i].Name < params[j].Name })
		for _, p := range params {
			val := ""
			if p.Value != nil {
				val = p.Value.String()
			}
			lines = append(lines, fmt.Sprintf("param:%s=%s", p.Name, val))
		}
	}

	// Artifacts — sorted for determinism; resolved to checksum where possible.
	if tmpl.Inputs.Artifacts != nil {
		arts := make([]wfv1.Artifact, len(tmpl.Inputs.Artifacts))
		copy(arts, tmpl.Inputs.Artifacts)
		sort.Slice(arts, func(i, j int) bool { return arts[i].Name < arts[j].Name })
		for _, art := range arts {
			identity, err := resolveArtifactIdentity(ctx, woc, ri, &art)
			if err != nil {
				woc.log.WithFields(logging.Fields{
					"artifactName": art.Name,
				}).WithError(err).Warn(ctx, "Could not resolve artifact identity for memoization key; falling back to path")
			}
			lines = append(lines, fmt.Sprintf("artifact:%s=%s", art.Name, identity))
		}
	}

	manifest := strings.Join(lines, "\n")
	h := sha256.Sum256([]byte(manifest))
	return hex.EncodeToString(h[:]), nil
}

// resolveArtifactIdentity returns the best available content identity for an
// artifact: its saved checksum, an ETag from the storage backend, or as a
// last resort the artifact's storage key (path).
func resolveArtifactIdentity(ctx context.Context, woc *wfOperationCtx, ri *wocResourceInterface, art *wfv1.Artifact) (string, error) {
	driver, err := artifacts.NewDriver(ctx, art, ri)
	if err != nil {
		// Fall back to key (path) if we can't build the driver.
		key, _ := art.GetKey()
		return key, fmt.Errorf("new driver: %w", err)
	}

	checksum, err := artifactcommon.ResolveChecksum(ctx, art, driver)
	if err != nil {
		key, _ := art.GetKey()
		return key, fmt.Errorf("resolve checksum: %w", err)
	}
	if checksum != "" {
		return checksum, nil
	}

	// No checksum or ETag available — use storage path as identity.
	key, err := art.GetKey()
	if err != nil {
		return art.Name, err
	}
	return key, nil
}
