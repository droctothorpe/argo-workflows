package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	wfv1 "github.com/argoproj/argo-workflows/v4/pkg/apis/workflow/v1alpha1"
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

// artifactIdentityFunc resolves the content identity for an artifact.
// It is a function type to allow testing without a full wfOperationCtx.
type artifactIdentityFunc func(ctx context.Context, art *wfv1.Artifact) (string, error)

// memoizationKey returns the effective cache key for the given template.
//
// If memoize.key is set by the user it is returned as-is (explicit override).
// Otherwise a deterministic key is computed from:
//   - the template name (scope isolation across templates)
//   - all resolved input parameters, sorted by name
//   - all resolved input artifacts, resolved to a checksum or ETag where
//     possible, falling back to the artifact storage path
//
// Parameters with only a ValueFrom source (no resolved Value) are included
// with an empty value, which may cause false cache misses but never false hits.
//
// Values are length-prefixed in the manifest to prevent injection via embedded
// newlines or delimiter characters.
//
// Any artifact whose checksum cannot be determined is silently fallen back to
// path-identity. The key derivation is intentionally non-fatal: worst case a
// false cache miss is returned (never a false hit).
func memoizationKey(ctx context.Context, woc *wfOperationCtx, tmpl *wfv1.Template) (string, error) {
	if tmpl.Memoize.Key != "" {
		return tmpl.Memoize.Key, nil
	}

	ri := &wocResourceInterface{woc: woc}
	identityFn := func(ctx context.Context, art *wfv1.Artifact) (string, error) {
		return resolveArtifactIdentity(ctx, woc, ri, art)
	}

	return buildMemoizationKey(ctx, tmpl, identityFn)
}

// buildMemoizationKey constructs a deterministic cache key from the template's
// name and resolved inputs. The artifactIdentityFn is called for each input
// artifact to resolve its content identity. This function is separated from
// memoizationKey for testability.
func buildMemoizationKey(ctx context.Context, tmpl *wfv1.Template, artifactIdentityFn artifactIdentityFunc) (string, error) {
	manifest := buildManifest(tmpl, func(art *wfv1.Artifact) string {
		// resolveArtifactIdentity always returns a non-empty fallback (storage key or
		// artifact name) even on error, so use the returned value unconditionally.
		identity, _ := artifactIdentityFn(ctx, art)
		return identity
	})

	h := sha256.Sum256([]byte(manifest))
	return hex.EncodeToString(h[:]), nil
}

// buildManifest creates a deterministic string manifest from the template's
// name, executor spec (image, command, script, etc.), and resolved inputs.
// The identityFn is called for each input artifact.
// This pure function contains no I/O and is directly unit-testable.
func buildManifest(tmpl *wfv1.Template, identityFn func(art *wfv1.Artifact) string) string {
	var lines []string

	// Version tag so future format changes produce distinct keys.
	lines = append(lines, "v1")

	// Template name scopes the key so two different templates with the same
	// inputs don't collide in the cache.
	lines = append(lines, "template:"+tmpl.Name)

	// Executor spec — what the step actually runs. Changes to image, command,
	// script body, etc. must bust the cache even when inputs are unchanged.
	// Omitted for template types with no executable spec (e.g. Suspend).
	if fp := executorFingerprint(tmpl); fp != "" {
		lines = append(lines, "executor:"+fp)
	}

	// Parameters — sorted by name for determinism.
	// Values are length-prefixed ("param:<name>=<len>:<value>") to prevent
	// newline injection from crafting colliding manifests.
	if tmpl.Inputs.Parameters != nil {
		sorted := sortedParameterNames(tmpl.Inputs.Parameters)
		for _, p := range sorted {
			val := ""
			if p.Value != nil {
				val = p.Value.String()
			}
			lines = append(lines, fmt.Sprintf("param:%s=%s:%s", p.Name, strconv.Itoa(len(val)), val))
		}
	}

	// Artifacts — sorted by name for determinism; resolved to checksum where possible.
	if tmpl.Inputs.Artifacts != nil {
		sorted := sortedArtifactNames(tmpl.Inputs.Artifacts)
		for _, art := range sorted {
			identity := identityFn(&art)
			lines = append(lines, fmt.Sprintf("artifact:%s=%s:%s", art.Name, strconv.Itoa(len(identity)), identity))
		}
	}

	return strings.Join(lines, "\n")
}

// containerLogicFields is a minimal representation of the fields that
// determine what a container actually executes. Resource requests, scheduling
// constraints, and other operational fields are intentionally excluded — they
// don't affect the correctness of the result, so changing them should not bust
// the cache.
type containerLogicFields struct {
	Image        string              `json:"image,omitempty"`
	Command      []string            `json:"command,omitempty"`
	Args         []string            `json:"args,omitempty"`
	Env          []apiv1.EnvVar      `json:"env,omitempty"`
	VolumeMounts []apiv1.VolumeMount `json:"volumeMounts,omitempty"`
}

// scriptLogicFields extends containerLogicFields with the script source body.
type scriptLogicFields struct {
	containerLogicFields
	Source string `json:"source,omitempty"`
}

// executorFingerprint returns a stable hash of only the fields that determine
// what the step computes. Operational fields (resource requests, nodeSelector,
// affinity, tolerations, retryStrategy, metrics, synchronization, etc.) are
// excluded so that tuning those does not bust the cache.
//
// For Container and Script templates the covered fields are:
//
//	image, command, args, env, volumeMounts (+ source for Script)
//
// For Resource, HTTP, Plugin, ContainerSet, DAG, Steps, and Data templates the
// entire sub-struct is marshalled since there is no obvious smaller subset.
func executorFingerprint(tmpl *wfv1.Template) string {
	var payload any
	switch {
	case tmpl.Container != nil:
		c := tmpl.Container
		payload = containerLogicFields{
			Image:        c.Image,
			Command:      c.Command,
			Args:         c.Args,
			Env:          c.Env,
			VolumeMounts: c.VolumeMounts,
		}
	case tmpl.Script != nil:
		s := tmpl.Script
		payload = scriptLogicFields{
			containerLogicFields: containerLogicFields{
				Image:        s.Image,
				Command:      s.Command,
				Args:         s.Args,
				Env:          s.Env,
				VolumeMounts: s.VolumeMounts,
			},
			Source: s.Source,
		}
	case tmpl.Resource != nil:
		payload = tmpl.Resource
	case tmpl.HTTP != nil:
		payload = tmpl.HTTP
	case tmpl.Plugin != nil:
		payload = tmpl.Plugin
	case tmpl.ContainerSet != nil:
		payload = tmpl.ContainerSet
	case tmpl.DAG != nil:
		payload = tmpl.DAG
	case tmpl.Steps != nil:
		payload = tmpl.Steps
	case tmpl.Data != nil:
		payload = tmpl.Data
	default:
		return ""
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf("type:%T", payload)
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// sortedParameterNames returns a copy of the parameters sorted by name.
func sortedParameterNames(params []wfv1.Parameter) []wfv1.Parameter {
	sorted := make([]wfv1.Parameter, len(params))
	copy(sorted, params)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	return sorted
}

// sortedArtifactNames returns a copy of the artifacts sorted by name.
func sortedArtifactNames(arts []wfv1.Artifact) []wfv1.Artifact {
	sorted := make([]wfv1.Artifact, len(arts))
	copy(sorted, arts)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	return sorted
}

// resolveArtifactIdentity returns the best available content identity for an
// artifact: its saved checksum, an ETag from the storage backend, or as a
// last resort the artifact's storage key (path). If the storage key is also
// unavailable, the artifact's name is returned.
func resolveArtifactIdentity(ctx context.Context, woc *wfOperationCtx, ri *wocResourceInterface, art *wfv1.Artifact) (string, error) {
	driver, err := artifacts.NewDriver(ctx, art, ri)
	if err != nil {
		// Fall back to storage key if we can't build the driver.
		key, keyErr := art.GetKey()
		if keyErr != nil {
			woc.log.WithError(keyErr).Warn(ctx, "Failed to get artifact storage key")
			return art.Name, fmt.Errorf("new driver: %w", err)
		}
		return key, fmt.Errorf("new driver: %w", err)
	}

	checksum, err := artifactcommon.ResolveChecksum(ctx, art, driver)
	if err != nil {
		key, keyErr := art.GetKey()
		if keyErr != nil {
			woc.log.WithError(keyErr).Warn(ctx, "Failed to get artifact storage key")
			return art.Name, fmt.Errorf("resolve checksum: %w", err)
		}
		return key, fmt.Errorf("resolve checksum: %w", err)
	}
	if checksum != "" {
		return checksum, nil
	}

	// No checksum or ETag available — use storage path as identity.
	key, err := art.GetKey()
	if err != nil {
		woc.log.WithError(err).Warn(ctx, "Failed to get artifact storage key; falling back to artifact name")
		return art.Name, err
	}
	return key, nil
}
