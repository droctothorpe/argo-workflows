package common

import (
	"context"
	"errors"
	"io"

	"github.com/argoproj/argo-workflows/v4/pkg/apis/workflow/v1alpha1"
)

// ArtifactDriver is the interface for loading and saving of artifacts
type ArtifactDriver interface {
	// Load accepts an artifact source URL and places it at specified path
	Load(ctx context.Context, inputArtifact *v1alpha1.Artifact, path string) error

	// OpenStream opens an artifact for reading. If the artifact is a file,
	// then the file should be opened. If the artifact is a directory, the
	// driver may return that as a tarball. OpenStream is intended to be efficient,
	// so implementations should minimise usage of disk, CPU and memory.
	// Implementations must not implement retry mechanisms. This will be handled by
	// the client, so would result in O(nm) cost.
	OpenStream(ctx context.Context, a *v1alpha1.Artifact) (io.ReadCloser, error)

	// Save uploads the path to artifact destination
	Save(ctx context.Context, path string, outputArtifact *v1alpha1.Artifact) error

	Delete(ctx context.Context, artifact *v1alpha1.Artifact) error

	ListObjects(ctx context.Context, artifact *v1alpha1.Artifact) ([]string, error)

	IsDirectory(ctx context.Context, artifact *v1alpha1.Artifact) (bool, error)
}

// ETagProvider is an optional interface implemented by artifact drivers that can return
// a stable content identifier for a stored artifact without downloading its content.
// The returned string is used as a change-detection fingerprint for memoization:
// it changes whenever the artifact content changes, but need not be a content hash.
// Drivers that do not implement this interface fall back to path-identity for caching.
type ETagProvider interface {
	// GetETag returns a stable identifier for the artifact content (e.g. an S3 ETag,
	// GCS generation number, or commit SHA for git artifacts).
	// Returns ("", nil) when the artifact is a directory or the identifier is unavailable.
	GetETag(ctx context.Context, artifact *v1alpha1.Artifact) (string, error)
}

// ResolveChecksum returns a checksum string suitable for use in a memoization cache key.
// If the artifact already has a Checksum set (computed at save time by the executor),
// it is returned unchanged. Otherwise, if the driver implements ETagProvider, the
// ETag is fetched and returned with an "etag:" prefix. Returns "" if neither is available
// (caller should fall back to path-identity or skip caching for this artifact).
func ResolveChecksum(ctx context.Context, artifact *v1alpha1.Artifact, driver ArtifactDriver) (string, error) {
	if artifact.Checksum != "" {
		return artifact.Checksum, nil
	}
	if p, ok := driver.(ETagProvider); ok {
		etag, err := p.GetETag(ctx, artifact)
		if err != nil {
			return "", err
		}
		if etag != "" {
			return "etag:" + etag, nil
		}
	}
	return "", nil
}

// ErrDeleteNotSupported Sentinel error definition for artifact deletion
var ErrDeleteNotSupported = errors.New("delete not supported for this artifact storage, please check" +
	" the following issue for details: https://github.com/argoproj/argo-workflows/issues/3102")
