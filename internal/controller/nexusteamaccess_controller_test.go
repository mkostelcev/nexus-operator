package controller

import (
	"context"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
	opmetrics "github.com/mkostelcev/nexus-operator/pkg/metrics"
)

func strPtr(s string) *string { return &s }

func TestBuildCSELExpression(t *testing.T) {
	tests := []struct {
		name       string
		format     string
		teamPath   string
		pathPrefix *string
		want       string
		wantErr    bool
	}{
		{
			name:     "docker format",
			format:   "docker",
			teamPath: "kostoed/integrations/alpha",
			want:     `path =~ "^/v2/(kostoed/integrations/alpha/.*)?$"`,
		},
		{
			name:     "maven2 format",
			format:   "maven2",
			teamPath: "kostoed/integrations/alpha",
			want:     `path =~ "^/ru/(kostoed/integrations/alpha/.*)?$"`,
		},
		{
			name:       "maven2 with custom pathPrefix",
			format:     "maven2",
			teamPath:   "kostoed/integrations/alpha",
			pathPrefix: strPtr("/"),
			want:       `path =~ "^/(kostoed/integrations/alpha/.*)?$"`,
		},
		{
			name:     "raw format",
			format:   "raw",
			teamPath: "kostoed/integrations/alpha",
			want:     `path =~ "^/(kostoed/integrations/alpha/.*)?$"`,
		},
		{
			name:     "npm format",
			format:   "npm",
			teamPath: "kostoed/integrations/alpha",
			want:     `path =~ "^@kostoed.integrations.alpha.*$"`,
		},
		{
			name:     "nuget format",
			format:   "nuget",
			teamPath: "kostoed/integrations/alpha",
			want:     `path =~ "^kostoed.integrations.alpha.*$"`,
		},
		{
			name:     "docker with leading/trailing slashes",
			format:   "docker",
			teamPath: "/kostoed/integrations/",
			want:     `path =~ "^/v2/(kostoed/integrations/.*)?$"`,
		},
		{
			name:    "unknown format",
			format:  "unknown",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildCSELExpression(tt.format, tt.teamPath, tt.pathPrefix)
			if tt.wantErr {
				if err == nil {
					t.Errorf("buildCSELExpression() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("buildCSELExpression() unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("buildCSELExpression() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeTeamName(t *testing.T) {
	tests := []struct {
		name     string
		teamPath string
		want     string
	}{
		{
			name:     "simple path",
			teamPath: "kostoed/integrations/alpha",
			want:     "kostoed-integrations-alpha",
		},
		{
			name:     "with leading slash",
			teamPath: "/kostoed/integrations/alpha",
			want:     "kostoed-integrations-alpha",
		},
		{
			name:     "with trailing slash",
			teamPath: "kostoed/integrations/alpha/",
			want:     "kostoed-integrations-alpha",
		},
		{
			name:     "uppercase mixed",
			teamPath: "Kostoed/Integrations/ALPHA",
			want:     "kostoed-integrations-alpha",
		},
		{
			name:     "single segment",
			teamPath: "myteam",
			want:     "myteam",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeTeamName(tt.teamPath)
			if got != tt.want {
				t.Errorf("sanitizeTeamName(%q) = %q, want %q", tt.teamPath, got, tt.want)
			}
		})
	}
}

func TestBuildDesiredResources(t *testing.T) {
	r := &NexusTeamAccessReconciler{}

	ta := &nexusv1alpha1.NexusTeamAccess{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "alpha-access",
			Namespace: "nexus",
		},
		Spec: nexusv1alpha1.NexusTeamAccessSpec{
			TeamPath: "kostoed/integrations/alpha",
			Repositories: []nexusv1alpha1.RepositoryGroup{
				{
					Format: "docker",
					Names:  []string{"registry"},
				},
				{
					Format: "maven2",
					Names:  []string{"releases", "snapshots"},
				},
			},
			AccessLevels: []string{"ro", "rw"},
		},
	}

	teamName := sanitizeTeamName(ta.Spec.TeamPath)
	cs, priv, perRepoRoles, aggRoles, err := r.buildDesiredResources(ta, teamName)
	if err != nil {
		t.Fatalf("buildDesiredResources() error: %v", err)
	}

	// 3 repos × 1 CS each = 3 content selectors
	if len(cs) != 3 {
		t.Errorf("expected 3 ContentSelectors, got %d", len(cs))
	}

	// 3 repos × 2 access levels = 6 privileges
	if len(priv) != 6 {
		t.Errorf("expected 6 Privileges, got %d", len(priv))
	}

	// 3 repos × 2 access levels = 6 per-repo roles
	if len(perRepoRoles) != 6 {
		t.Errorf("expected 6 per-repo Roles, got %d", len(perRepoRoles))
	}

	// 2 access levels = 2 aggregate roles
	if len(aggRoles) != 2 {
		t.Errorf("expected 2 aggregate Roles, got %d", len(aggRoles))
	}

	// Verify CS naming
	for _, c := range cs {
		if !strings.HasPrefix(c.Spec.Name, "cs-kostoed-integrations-alpha-") {
			t.Errorf("unexpected CS name: %s", c.Spec.Name)
		}
	}

	// Verify privilege naming
	for _, p := range priv {
		if !strings.HasPrefix(p.Spec.Name, "cs-kostoed-integrations-alpha-") {
			t.Errorf("unexpected Privilege name: %s", p.Spec.Name)
		}
		if !strings.HasSuffix(p.Spec.Name, "-ro") && !strings.HasSuffix(p.Spec.Name, "-rw") {
			t.Errorf("privilege name should end with access level: %s", p.Spec.Name)
		}
	}

	// Verify per-repo role naming
	for _, role := range perRepoRoles {
		if !strings.HasPrefix(role.Spec.RoleID, "nx-kostoed-integrations-alpha-") {
			t.Errorf("unexpected per-repo role ID: %s", role.Spec.RoleID)
		}
	}

	// Verify aggregate role naming
	for _, role := range aggRoles {
		if !strings.HasPrefix(role.Spec.RoleID, "nexus-kostoed-integrations-alpha-") {
			t.Errorf("unexpected aggregate role ID: %s", role.Spec.RoleID)
		}
	}

	// Verify labels on all resources
	for _, c := range cs {
		if c.Labels[labelTeamAccess] != "alpha-access" {
			t.Errorf("CS %s missing team-access label", c.Name)
		}
		if c.Labels[labelManagedBy] != managedByValue {
			t.Errorf("CS %s missing managed-by label", c.Name)
		}
	}
}

func TestRoleHierarchy(t *testing.T) {
	r := &NexusTeamAccessReconciler{}

	ta := &nexusv1alpha1.NexusTeamAccess{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-access",
			Namespace: "nexus",
		},
		Spec: nexusv1alpha1.NexusTeamAccessSpec{
			TeamPath: "myteam",
			Repositories: []nexusv1alpha1.RepositoryGroup{
				{Format: "docker", Names: []string{"registry"}},
			},
			AccessLevels: []string{"ro", "rw"},
		},
	}

	teamName := sanitizeTeamName(ta.Spec.TeamPath)
	_, _, perRepoRoles, aggRoles, err := r.buildDesiredResources(ta, teamName)
	if err != nil {
		t.Fatalf("buildDesiredResources() error: %v", err)
	}

	// Find rw per-repo role — should include ro as subrole
	var rwPerRepo *nexusv1alpha1.Role
	for i, role := range perRepoRoles {
		if strings.HasSuffix(role.Spec.RoleID, "-rw") {
			rwPerRepo = &perRepoRoles[i]
			break
		}
	}
	if rwPerRepo == nil {
		t.Fatal("rw per-repo role not found")
	}
	if len(rwPerRepo.Spec.Roles) == 0 {
		t.Error("rw per-repo role should have ro as subrole")
	} else {
		found := false
		for _, sub := range rwPerRepo.Spec.Roles {
			if strings.HasSuffix(sub, "-ro") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("rw per-repo role subroles %v should include ro role", rwPerRepo.Spec.Roles)
		}
	}

	// Find ro per-repo role — should have no subroles
	var roPerRepo *nexusv1alpha1.Role
	for i, role := range perRepoRoles {
		if strings.HasSuffix(role.Spec.RoleID, "-ro") {
			roPerRepo = &perRepoRoles[i]
			break
		}
	}
	if roPerRepo == nil {
		t.Fatal("ro per-repo role not found")
	}
	if len(roPerRepo.Spec.Roles) != 0 {
		t.Errorf("ro per-repo role should have no subroles, got %v", roPerRepo.Spec.Roles)
	}

	// Find aggregate rw — should include aggregate ro as subrole
	var rwAgg *nexusv1alpha1.Role
	for i, role := range aggRoles {
		if strings.HasSuffix(role.Spec.RoleID, "-rw") {
			rwAgg = &aggRoles[i]
			break
		}
	}
	if rwAgg == nil {
		t.Fatal("rw aggregate role not found")
	}
	foundAggRo := false
	for _, sub := range rwAgg.Spec.Roles {
		if strings.HasSuffix(sub, "-ro") {
			foundAggRo = true
			break
		}
	}
	if !foundAggRo {
		t.Errorf("rw aggregate role subroles %v should include aggregate ro", rwAgg.Spec.Roles)
	}

	// Find aggregate ro — should NOT include any aggregate subrole
	var roAgg *nexusv1alpha1.Role
	for i, role := range aggRoles {
		if strings.HasSuffix(role.Spec.RoleID, "-ro") {
			roAgg = &aggRoles[i]
			break
		}
	}
	if roAgg == nil {
		t.Fatal("ro aggregate role not found")
	}
	for _, sub := range roAgg.Spec.Roles {
		if strings.HasPrefix(sub, "nexus-") {
			t.Errorf("ro aggregate role should not have aggregate subroles, got %s", sub)
		}
	}
}

func TestRoleHierarchyRWD(t *testing.T) {
	r := &NexusTeamAccessReconciler{}

	ta := &nexusv1alpha1.NexusTeamAccess{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rwd",
			Namespace: "nexus",
		},
		Spec: nexusv1alpha1.NexusTeamAccessSpec{
			TeamPath: "myteam",
			Repositories: []nexusv1alpha1.RepositoryGroup{
				{Format: "raw", Names: []string{"storage"}},
			},
			AccessLevels: []string{"ro", "rw", "rwd"},
		},
	}

	teamName := sanitizeTeamName(ta.Spec.TeamPath)
	_, _, perRepoRoles, aggRoles, err := r.buildDesiredResources(ta, teamName)
	if err != nil {
		t.Fatalf("buildDesiredResources() error: %v", err)
	}

	// 1 repo × 3 levels = 3 per-repo roles
	if len(perRepoRoles) != 3 {
		t.Errorf("expected 3 per-repo roles, got %d", len(perRepoRoles))
	}

	// 3 aggregate roles
	if len(aggRoles) != 3 {
		t.Errorf("expected 3 aggregate roles, got %d", len(aggRoles))
	}

	// rwd per-repo should have rw as subrole
	for _, role := range perRepoRoles {
		if strings.HasSuffix(role.Spec.RoleID, "-rwd") {
			found := false
			for _, sub := range role.Spec.Roles {
				if strings.HasSuffix(sub, "-rw") {
					found = true
				}
			}
			if !found {
				t.Errorf("rwd per-repo role should have rw subrole, got %v", role.Spec.Roles)
			}
		}
	}

	// rwd aggregate should have rw aggregate as subrole
	for _, role := range aggRoles {
		if strings.HasSuffix(role.Spec.RoleID, "-rwd") {
			found := false
			for _, sub := range role.Spec.Roles {
				if strings.Contains(sub, "-rw") && !strings.Contains(sub, "-rwd") {
					found = true
				}
			}
			if !found {
				t.Errorf("rwd aggregate role should include rw aggregate, got %v", role.Spec.Roles)
			}
		}
	}
}

func TestBuildDesiredResourcesPerRepoGroupLevels(t *testing.T) {
	r := &NexusTeamAccessReconciler{}

	ta := &nexusv1alpha1.NexusTeamAccess{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mixed-levels",
			Namespace: "nexus",
		},
		Spec: nexusv1alpha1.NexusTeamAccessSpec{
			TeamPath: "team/test",
			Repositories: []nexusv1alpha1.RepositoryGroup{
				{
					Format:       "maven2",
					Names:        []string{"maven-public"},
					AccessLevels: []string{"ro"}, // override: only ro
				},
				{
					Format: "docker",
					Names:  []string{"registry"},
					// no override: inherits global [ro, rw]
				},
			},
			AccessLevels: []string{"ro", "rw"},
		},
	}

	teamName := sanitizeTeamName(ta.Spec.TeamPath)
	cs, priv, perRepoRoles, aggRoles, err := r.buildDesiredResources(ta, teamName)
	if err != nil {
		t.Fatalf("buildDesiredResources() error: %v", err)
	}

	// 2 repos = 2 content selectors
	if len(cs) != 2 {
		t.Errorf("expected 2 ContentSelectors, got %d", len(cs))
	}

	// maven-public: 1 priv (ro), registry: 2 privs (ro, rw) = 3 total
	if len(priv) != 3 {
		t.Errorf("expected 3 Privileges, got %d", len(priv))
	}

	// 3 per-repo roles (maven-public-ro, registry-ro, registry-rw)
	if len(perRepoRoles) != 3 {
		t.Errorf("expected 3 per-repo Roles, got %d", len(perRepoRoles))
	}

	// 2 aggregate roles (ro, rw)
	if len(aggRoles) != 2 {
		t.Errorf("expected 2 aggregate Roles, got %d", len(aggRoles))
	}

	// Verify: no maven-public-rw privilege exists
	for _, p := range priv {
		if strings.Contains(p.Spec.Name, "maven-public") && strings.HasSuffix(p.Spec.Name, "-rw") {
			t.Errorf("maven-public should NOT have rw privilege, got: %s", p.Spec.Name)
		}
	}

	// Verify: no maven-public-rw per-repo role exists
	for _, role := range perRepoRoles {
		if strings.Contains(role.Spec.RoleID, "maven-public") && strings.HasSuffix(role.Spec.RoleID, "-rw") {
			t.Errorf("maven-public should NOT have rw per-repo role, got: %s", role.Spec.RoleID)
		}
	}

	// Verify aggregate ro has both per-repo ro roles
	var roAgg *nexusv1alpha1.Role
	for i, role := range aggRoles {
		if strings.HasSuffix(role.Spec.RoleID, "-ro") {
			roAgg = &aggRoles[i]
			break
		}
	}
	if roAgg == nil {
		t.Fatal("ro aggregate role not found")
	}
	if len(roAgg.Spec.Roles) != 2 {
		t.Errorf("ro aggregate should have 2 per-repo roles (maven-public-ro + registry-ro), got %d: %v", len(roAgg.Spec.Roles), roAgg.Spec.Roles)
	}

	// Verify aggregate rw has only registry-rw per-repo role + aggregate ro
	var rwAgg *nexusv1alpha1.Role
	for i, role := range aggRoles {
		if strings.HasSuffix(role.Spec.RoleID, "-rw") {
			rwAgg = &aggRoles[i]
			break
		}
	}
	if rwAgg == nil {
		t.Fatal("rw aggregate role not found")
	}
	// Should contain: nx-team-test-docker-registry-rw + nexus-team-test-ro
	if len(rwAgg.Spec.Roles) != 2 {
		t.Errorf("rw aggregate should have 2 subroles (registry-rw + aggregate-ro), got %d: %v", len(rwAgg.Spec.Roles), rwAgg.Spec.Roles)
	}
	hasRegistryRw := false
	hasAggRo := false
	for _, sub := range rwAgg.Spec.Roles {
		if strings.Contains(sub, "docker-registry-rw") {
			hasRegistryRw = true
		}
		if strings.HasPrefix(sub, "nexus-") && strings.HasSuffix(sub, "-ro") {
			hasAggRo = true
		}
	}
	if !hasRegistryRw {
		t.Errorf("rw aggregate should include docker-registry-rw, got: %v", rwAgg.Spec.Roles)
	}
	if !hasAggRo {
		t.Errorf("rw aggregate should include aggregate ro, got: %v", rwAgg.Spec.Roles)
	}
}

func TestRepoViewPrivileges(t *testing.T) {
	tests := []struct {
		level string
		want  []string
	}{
		{"ro", []string{"nx-repository-view-maven2-releases-browse", "nx-repository-view-maven2-releases-read"}},
		{"rw", []string{"nx-repository-view-maven2-releases-browse", "nx-repository-view-maven2-releases-read", "nx-repository-view-maven2-releases-add", "nx-repository-view-maven2-releases-edit"}},
		{"rwd", []string{"nx-repository-view-maven2-releases-browse", "nx-repository-view-maven2-releases-read", "nx-repository-view-maven2-releases-add", "nx-repository-view-maven2-releases-edit", "nx-repository-view-maven2-releases-delete"}},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			got := repoViewPrivileges("maven2", "releases", tt.level)
			if len(got) != len(tt.want) {
				t.Fatalf("repoViewPrivileges(%q) returned %d items, want %d", tt.level, len(got), len(tt.want))
			}
			for i, p := range got {
				if p != tt.want[i] {
					t.Errorf("repoViewPrivileges(%q)[%d] = %q, want %q", tt.level, i, p, tt.want[i])
				}
			}
		})
	}
}

func TestPerRepoRoleContainsViewPrivileges(t *testing.T) {
	r := &NexusTeamAccessReconciler{}

	ta := &nexusv1alpha1.NexusTeamAccess{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "view-privs-test",
			Namespace: "nexus",
		},
		Spec: nexusv1alpha1.NexusTeamAccessSpec{
			TeamPath: "myteam",
			Repositories: []nexusv1alpha1.RepositoryGroup{
				{Format: "docker", Names: []string{"registry"}},
			},
			AccessLevels: []string{"ro"},
		},
	}

	teamName := sanitizeTeamName(ta.Spec.TeamPath)
	_, _, perRepoRoles, _, err := r.buildDesiredResources(ta, teamName)
	if err != nil {
		t.Fatalf("buildDesiredResources() error: %v", err)
	}

	if len(perRepoRoles) != 1 {
		t.Fatalf("expected 1 per-repo role, got %d", len(perRepoRoles))
	}

	role := perRepoRoles[0]
	// Should have: CS privilege + browse + read = 3
	if len(role.Spec.Privileges) != 3 {
		t.Errorf("expected 3 privileges in per-repo role, got %d: %v", len(role.Spec.Privileges), role.Spec.Privileges)
	}

	wantPrivs := map[string]bool{
		"cs-myteam-docker-registry-ro":              true,
		"nx-repository-view-docker-registry-browse": true,
		"nx-repository-view-docker-registry-read":   true,
	}
	for _, p := range role.Spec.Privileges {
		if !wantPrivs[p] {
			t.Errorf("unexpected privilege in per-repo role: %s", p)
		}
	}
}

func TestAccessLevelActions(t *testing.T) {
	tests := []struct {
		level   string
		actions []string
	}{
		{"ro", []string{"READ", "BROWSE"}},
		{"rw", []string{"READ", "BROWSE", "ADD", "EDIT"}},
		{"rwd", []string{"READ", "BROWSE", "ADD", "EDIT", "DELETE"}},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			actions, ok := accessLevelActions[tt.level]
			if !ok {
				t.Fatalf("access level %q not found", tt.level)
			}
			if len(actions) != len(tt.actions) {
				t.Errorf("expected %d actions, got %d", len(tt.actions), len(actions))
			}
			for i, a := range actions {
				if a != tt.actions[i] {
					t.Errorf("action[%d] = %q, want %q", i, a, tt.actions[i])
				}
			}
		})
	}
}

// Единственный, кроме repository, путь удаления, достижимый без сети: ветка
// DeletionTimestamp в Reconcile отрабатывает до похода в Nexus и Keycloak.
// Проверяем, что снятие финализатора убирает серию nexus_operator_resource_ready,
// иначе алерт по удалённому CR висел бы вечно.
func TestReconcileDeletionRemovesResourceReady(t *testing.T) {
	opmetrics.ResourceReady.Reset()
	t.Cleanup(opmetrics.ResourceReady.Reset)

	now := metav1.Now()
	ta := &nexusv1alpha1.NexusTeamAccess{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "team-alpha",
			Namespace:         "nexus",
			DeletionTimestamp: &now,
			Finalizers:        []string{teamAccessFinalizer},
		},
	}
	scheme := runtime.NewScheme()
	require.NoError(t, nexusv1alpha1.AddToScheme(scheme))
	r := &NexusTeamAccessReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(ta).
			WithStatusSubresource(ta).
			Build(),
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	// Серия удаляемого ресурса и серия соседа, который обязан уцелеть.
	opmetrics.SetResourceReady("nexusteamaccess", "nexus", "team-alpha", false)
	opmetrics.SetResourceReady("nexusteamaccess", "nexus", "team-other", true)

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: client.ObjectKeyFromObject(ta),
	})
	require.NoError(t, err)

	// Финализатор снят — ветка удаления действительно пройдена. Объект после
	// снятия последнего финализатора исчезает из fake-клиента.
	var after nexusv1alpha1.NexusTeamAccess
	getErr := r.Get(context.Background(), client.ObjectKeyFromObject(ta), &after)
	if getErr == nil {
		require.NotContains(t, after.Finalizers, teamAccessFinalizer,
			"финализатор не снят — тест не дошёл до ветки удаления")
	} else {
		require.True(t, k8serrors.IsNotFound(getErr), "неожиданная ошибка чтения CR: %v", getErr)
	}

	assertResourceReadySeries(t,
		`nexus_operator_resource_ready{name="team-other",resource_namespace="nexus",type="nexusteamaccess"} 1`,
	)
}
