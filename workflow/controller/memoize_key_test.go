package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	wfv1 "github.com/argoproj/argo-workflows/v4/pkg/apis/workflow/v1alpha1"
)

func makeTemplate(name string, params []wfv1.Parameter, arts []wfv1.Artifact) *wfv1.Template {
	return &wfv1.Template{
		Name: name,
		Inputs: wfv1.Inputs{
			Parameters: params,
			Artifacts:  arts,
		},
	}
}

func hashManifest(manifest string) string {
	h := sha256.Sum256([]byte(manifest))
	return hex.EncodeToString(h[:])
}

func stubIdentity(identity string) func(art *wfv1.Artifact) string {
	return func(art *wfv1.Artifact) string { return identity }
}

func TestBuildManifest_Deterministic(t *testing.T) {
	tmpl := makeTemplate("my-template",
		[]wfv1.Parameter{
			{Name: "b-param", Value: wfv1.AnyStringPtr("val-b")},
			{Name: "a-param", Value: wfv1.AnyStringPtr("val-a")},
		},
		nil,
	)

	m1 := buildManifest(tmpl, stubIdentity(""))
	m2 := buildManifest(tmpl, stubIdentity(""))
	assert.Equal(t, m1, m2, "manifest should be deterministic across calls")
}

func TestBuildManifest_ParametersSortedByName(t *testing.T) {
	tmpl := makeTemplate("tmpl",
		[]wfv1.Parameter{
			{Name: "z", Value: wfv1.AnyStringPtr("1")},
			{Name: "a", Value: wfv1.AnyStringPtr("2")},
			{Name: "m", Value: wfv1.AnyStringPtr("3")},
		},
		nil,
	)

	manifest := buildManifest(tmpl, stubIdentity(""))
	assert.Contains(t, manifest, "param:a=1:2\nparam:m=1:3\nparam:z=1:1")
}

func TestBuildManifest_ArtifactsSortedByName(t *testing.T) {
	tmpl := makeTemplate("tmpl",
		nil,
		[]wfv1.Artifact{
			{Name: "z-art"},
			{Name: "a-art"},
		},
	)

	manifest := buildManifest(tmpl, func(art *wfv1.Artifact) string {
		return "checksum-" + art.Name
	})
	assert.Contains(t, manifest, "artifact:a-art=")
	assert.Contains(t, manifest, "artifact:z-art=")
	// a-art should come before z-art
	aIdx := len("v1\ntemplate:tmpl\n")
	assert.Equal(t, "artifact:a-art=", manifest[aIdx:aIdx+len("artifact:a-art=")])
}

func TestBuildManifest_NewlineInjectionPrevented(t *testing.T) {
	// Craft two templates that would collide without length-prefixing:
	// Template A: param x = "line1\nparam:y=injected"
	// Template B: param x = "line1", param y = "injected"
	tmplA := makeTemplate("tmpl",
		[]wfv1.Parameter{
			{Name: "x", Value: wfv1.AnyStringPtr("line1\nparam:y=injected")},
		},
		nil,
	)
	tmplB := makeTemplate("tmpl",
		[]wfv1.Parameter{
			{Name: "x", Value: wfv1.AnyStringPtr("line1")},
			{Name: "y", Value: wfv1.AnyStringPtr("injected")},
		},
		nil,
	)

	mA := buildManifest(tmplA, stubIdentity(""))
	mB := buildManifest(tmplB, stubIdentity(""))
	assert.NotEqual(t, mA, mB, "manifests with newline injection should differ due to length-prefixing")
	assert.NotEqual(t, hashManifest(mA), hashManifest(mB))
}

func TestBuildManifest_DifferentTemplateNamesDiffer(t *testing.T) {
	params := []wfv1.Parameter{{Name: "x", Value: wfv1.AnyStringPtr("v")}}
	m1 := buildManifest(makeTemplate("tmpl-a", params, nil), stubIdentity(""))
	m2 := buildManifest(makeTemplate("tmpl-b", params, nil), stubIdentity(""))
	assert.NotEqual(t, m1, m2)
}

func TestBuildManifest_NilValueParameter(t *testing.T) {
	// Parameters with Value == nil (e.g. ValueFrom-only) should produce an empty value.
	tmpl := makeTemplate("tmpl",
		[]wfv1.Parameter{
			{Name: "resolved", Value: wfv1.AnyStringPtr("hello")},
			{Name: "unresolved"}, // Value is nil
		},
		nil,
	)

	manifest := buildManifest(tmpl, stubIdentity(""))
	assert.Contains(t, manifest, "param:resolved=5:hello")
	assert.Contains(t, manifest, "param:unresolved=0:")
}

func TestBuildManifest_VersionPrefix(t *testing.T) {
	tmpl := makeTemplate("tmpl", nil, nil)
	manifest := buildManifest(tmpl, stubIdentity(""))
	assert.True(t, len(manifest) >= 2 && manifest[:2] == "v1", "manifest should start with version prefix")
}

func TestBuildManifest_EmptyInputs(t *testing.T) {
	tmpl := makeTemplate("tmpl", nil, nil)
	manifest := buildManifest(tmpl, stubIdentity(""))
	assert.Equal(t, "v1\ntemplate:tmpl", manifest)
}

func TestBuildManifest_DoesNotMutateInput(t *testing.T) {
	params := []wfv1.Parameter{
		{Name: "z", Value: wfv1.AnyStringPtr("1")},
		{Name: "a", Value: wfv1.AnyStringPtr("2")},
	}
	tmpl := makeTemplate("tmpl", params, nil)

	buildManifest(tmpl, stubIdentity(""))

	// Original order should be preserved.
	require.Len(t, tmpl.Inputs.Parameters, 2)
	assert.Equal(t, "z", tmpl.Inputs.Parameters[0].Name)
	assert.Equal(t, "a", tmpl.Inputs.Parameters[1].Name)
}

func TestBuildManifest_ArtifactIdentityUsed(t *testing.T) {
	tmpl := makeTemplate("tmpl",
		nil,
		[]wfv1.Artifact{
			{Name: "data"},
		},
	)

	manifest := buildManifest(tmpl, func(art *wfv1.Artifact) string {
		return "sha256:abc123"
	})
	assert.Contains(t, manifest, "artifact:data=13:sha256:abc123")
}
