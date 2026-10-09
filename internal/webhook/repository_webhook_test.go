package webhook

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
)

// newRepository creates a Repository with the given type and optional configs applied.
func newRepository(repoType string, opts ...func(*nexusv1alpha1.RepositorySpec)) *nexusv1alpha1.Repository {
	repo := &nexusv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-repo",
			Namespace: "default",
		},
		Spec: nexusv1alpha1.RepositorySpec{
			Name:   "test-repo",
			Type:   repoType,
			Online: true,
			Storage: nexusv1alpha1.StorageConfig{
				BlobStoreName:               "default",
				StrictContentTypeValidation: true,
				WritePolicy:                 "ALLOW",
			},
		},
	}
	for _, fn := range opts {
		fn(&repo.Spec)
	}
	return repo
}

func withMaven() func(*nexusv1alpha1.RepositorySpec) {
	return func(s *nexusv1alpha1.RepositorySpec) {
		s.Maven = &nexusv1alpha1.MavenConfig{
			VersionPolicy: "RELEASE",
			LayoutPolicy:  "STRICT",
		}
	}
}

func withDocker() func(*nexusv1alpha1.RepositorySpec) {
	return func(s *nexusv1alpha1.RepositorySpec) {
		s.Docker = &nexusv1alpha1.DockerConfig{
			V1Enabled: false,
		}
	}
}

func withProxy(remoteUrl string) func(*nexusv1alpha1.RepositorySpec) {
	return func(s *nexusv1alpha1.RepositorySpec) {
		s.Proxy = &nexusv1alpha1.ProxyConfig{
			RemoteUrl: remoteUrl,
		}
	}
}

func withProxyEmpty() func(*nexusv1alpha1.RepositorySpec) {
	return func(s *nexusv1alpha1.RepositorySpec) {
		s.Proxy = &nexusv1alpha1.ProxyConfig{}
	}
}

func withGroup(members ...string) func(*nexusv1alpha1.RepositorySpec) {
	return func(s *nexusv1alpha1.RepositorySpec) {
		s.Group = &nexusv1alpha1.GroupConfig{
			MemberNames: members,
		}
	}
}

func withGroupEmpty() func(*nexusv1alpha1.RepositorySpec) {
	return func(s *nexusv1alpha1.RepositorySpec) {
		s.Group = &nexusv1alpha1.GroupConfig{
			MemberNames: []string{},
		}
	}
}

func withYum() func(*nexusv1alpha1.RepositorySpec) {
	return func(s *nexusv1alpha1.RepositorySpec) {
		s.Yum = &nexusv1alpha1.YumConfig{
			RepodataDepth: 0,
		}
	}
}

// ---------------------------------------------------------------------------
// Tests for ValidateCreate
// ---------------------------------------------------------------------------

func TestValidateCreate_WrongObjectType(t *testing.T) {
	v := &RepositoryValidator{}
	wrongObj := &unstructured.Unstructured{}

	_, err := v.ValidateCreate(context.Background(), wrongObj)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, errExpectedRepository))
}

func TestValidateCreate_ValidRepository(t *testing.T) {
	v := &RepositoryValidator{}
	repo := newRepository("maven-hosted", withMaven())

	_, err := v.ValidateCreate(context.Background(), repo)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Tests for ValidateUpdate
// ---------------------------------------------------------------------------

func TestValidateUpdate_WrongObjectType(t *testing.T) {
	v := &RepositoryValidator{}
	old := newRepository("maven-hosted", withMaven())
	wrongObj := &unstructured.Unstructured{}

	_, err := v.ValidateUpdate(context.Background(), old, wrongObj)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, errExpectedRepository))
}

func TestValidateUpdate_ValidRepository(t *testing.T) {
	v := &RepositoryValidator{}
	old := newRepository("maven-hosted", withMaven())
	updated := newRepository("maven-hosted", withMaven())

	_, err := v.ValidateUpdate(context.Background(), old, updated)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Tests for ValidateDelete
// ---------------------------------------------------------------------------

func TestValidateDelete_AlwaysNil(t *testing.T) {
	v := &RepositoryValidator{}

	_, err := v.ValidateDelete(context.Background(), nil)
	assert.NoError(t, err)

	repo := newRepository("maven-hosted", withMaven())
	_, err = v.ValidateDelete(context.Background(), repo)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Tests for validateRepository (via ValidateCreate) - table driven
// ---------------------------------------------------------------------------

func TestValidateRepository(t *testing.T) {
	tests := []struct {
		name      string
		repo      *nexusv1alpha1.Repository
		wantErr   error
		expectNil bool
	}{
		// ----- Maven hosted -----
		{
			name:      "valid maven-hosted with maven config",
			repo:      newRepository("maven-hosted", withMaven()),
			expectNil: true,
		},
		{
			name:    "maven-hosted missing maven config",
			repo:    newRepository("maven-hosted"),
			wantErr: errMavenRequired,
		},
		{
			name:    "maven-hosted with proxy set",
			repo:    newRepository("maven-hosted", withMaven(), withProxy("https://example.com")),
			wantErr: errProxyMustBeEmpty,
		},
		{
			name:    "maven-hosted with group set",
			repo:    newRepository("maven-hosted", withMaven(), withGroup("member1")),
			wantErr: errGroupMustBeEmpty,
		},

		// ----- Maven proxy -----
		{
			name:      "valid maven-proxy with proxy and maven",
			repo:      newRepository("maven-proxy", withMaven(), withProxy("https://repo1.maven.org/maven2/")),
			expectNil: true,
		},
		{
			name:    "maven-proxy missing proxy",
			repo:    newRepository("maven-proxy", withMaven()),
			wantErr: errProxyRequired,
		},
		{
			name:    "maven-proxy with proxy but empty RemoteUrl",
			repo:    newRepository("maven-proxy", withMaven(), withProxyEmpty()),
			wantErr: errProxyRemoteUrlRequired,
		},
		{
			name:    "maven-proxy missing maven config",
			repo:    newRepository("maven-proxy", withProxy("https://repo1.maven.org/maven2/")),
			wantErr: errMavenRequired,
		},

		// ----- Maven group -----
		{
			name:      "valid maven-group with group members",
			repo:      newRepository("maven-group", withGroup("maven-releases", "maven-snapshots")),
			expectNil: true,
		},
		{
			name:      "maven-group without maven config is valid (maven optional for group)",
			repo:      newRepository("maven-group", withGroup("member1")),
			expectNil: true,
		},
		{
			name:    "maven-group missing group",
			repo:    newRepository("maven-group", withMaven()),
			wantErr: errGroupRequired,
		},
		{
			name:    "maven-group with group but empty MemberNames",
			repo:    newRepository("maven-group", withGroupEmpty()),
			wantErr: errGroupMemberNamesEmpty,
		},

		// ----- Docker hosted -----
		{
			name:      "valid docker-hosted with docker config",
			repo:      newRepository("docker-hosted", withDocker()),
			expectNil: true,
		},
		{
			name:    "docker-hosted missing docker config",
			repo:    newRepository("docker-hosted"),
			wantErr: errDockerRequired,
		},

		// ----- Docker proxy -----
		{
			name:      "valid docker-proxy with docker and proxy",
			repo:      newRepository("docker-proxy", withDocker(), withProxy("https://registry-1.docker.io")),
			expectNil: true,
		},
		{
			name:    "docker-proxy missing docker config",
			repo:    newRepository("docker-proxy", withProxy("https://registry-1.docker.io")),
			wantErr: errDockerRequired,
		},

		// ----- Docker group -----
		{
			name:      "valid docker-group with docker and group",
			repo:      newRepository("docker-group", withDocker(), withGroup("docker-hosted", "docker-proxy")),
			expectNil: true,
		},
		{
			name:    "docker-group missing docker config",
			repo:    newRepository("docker-group", withGroup("docker-hosted")),
			wantErr: errDockerRequired,
		},

		// ----- Yum hosted -----
		{
			name:      "valid yum-hosted with yum config",
			repo:      newRepository("yum-hosted", withYum()),
			expectNil: true,
		},
		{
			name:    "yum-hosted missing yum config",
			repo:    newRepository("yum-hosted"),
			wantErr: errYumRequired,
		},

		// ----- NPM (no format-specific requirements) -----
		{
			name:      "npm-hosted no extra config required",
			repo:      newRepository("npm-hosted"),
			expectNil: true,
		},

		// ----- Raw proxy -----
		{
			name:      "raw-proxy with proxy is valid",
			repo:      newRepository("raw-proxy", withProxy("https://example.com/raw")),
			expectNil: true,
		},

		// ----- Helm hosted -----
		{
			name:      "helm-hosted no extra config required",
			repo:      newRepository("helm-hosted"),
			expectNil: true,
		},
	}

	v := &RepositoryValidator{}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.ValidateCreate(context.Background(), tc.repo)

			if tc.expectNil {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, tc.wantErr),
					"expected error wrapping %v, got: %v", tc.wantErr, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests for helper functions
// ---------------------------------------------------------------------------

func TestIsProxyType(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"maven-proxy", true},
		{"docker-proxy", true},
		{"npm-proxy", true},
		{"raw-proxy", true},
		{"maven-hosted", false},
		{"maven-group", false},
		{"proxy", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, isProxyType(tc.input))
		})
	}
}

func TestIsGroupType(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"npm-group", true},
		{"maven-group", true},
		{"docker-group", true},
		{"npm-proxy", false},
		{"npm-hosted", false},
		{"group", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, isGroupType(tc.input))
		})
	}
}

func TestIsHostedType(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"docker-hosted", true},
		{"maven-hosted", true},
		{"npm-hosted", true},
		{"docker-group", false},
		{"docker-proxy", false},
		{"hosted", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, isHostedType(tc.input))
		})
	}
}

func TestIsMavenType(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"maven-hosted", true},
		{"maven-proxy", true},
		{"maven-group", true},
		{"npm-hosted", false},
		{"docker-proxy", false},
		{"maven", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, isMavenType(tc.input))
		})
	}
}

func TestIsDockerType(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"docker-proxy", true},
		{"docker-hosted", true},
		{"docker-group", true},
		{"maven-proxy", false},
		{"npm-hosted", false},
		{"docker", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, isDockerType(tc.input))
		})
	}
}
