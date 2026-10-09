package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
)

func TestExtractAggregateRoleNames(t *testing.T) {
	ta := &nexusv1alpha1.NexusTeamAccess{
		Status: nexusv1alpha1.NexusTeamAccessStatus{
			GeneratedResources: []nexusv1alpha1.GeneratedResource{
				{Kind: "ContentSelector", Name: "cs-team-docker-registry", NexusName: "cs-team-docker-registry"},
				{Kind: "Privilege", Name: "cs-team-docker-registry-ro", NexusName: "cs-team-docker-registry-ro"},
				{Kind: "Role", Name: "nx-team-docker-registry-ro", NexusName: "nx-team-docker-registry-ro"},
				{Kind: "Role", Name: "nexus-team-ro", NexusName: "nexus-team-ro"},
				{Kind: "Role", Name: "nexus-team-rw", NexusName: "nexus-team-rw"},
			},
		},
	}

	names := extractAggregateRoleNames(ta)

	if len(names) != 2 {
		t.Fatalf("expected 2 aggregate roles, got %d: %v", len(names), names)
	}

	expected := map[string]bool{"nexus-team-ro": true, "nexus-team-rw": true}
	for _, name := range names {
		if !expected[name] {
			t.Errorf("unexpected aggregate role: %s", name)
		}
	}
}

func TestExtractAggregateRoleNamesEmpty(t *testing.T) {
	ta := &nexusv1alpha1.NexusTeamAccess{
		Status: nexusv1alpha1.NexusTeamAccessStatus{},
	}

	names := extractAggregateRoleNames(ta)
	if len(names) != 0 {
		t.Errorf("expected 0 aggregate roles, got %d", len(names))
	}
}

func TestExtractAggregateRoleNamesNoAggregates(t *testing.T) {
	ta := &nexusv1alpha1.NexusTeamAccess{
		Status: nexusv1alpha1.NexusTeamAccessStatus{
			GeneratedResources: []nexusv1alpha1.GeneratedResource{
				{Kind: "Role", Name: "nx-team-docker-registry-ro", NexusName: "nx-team-docker-registry-ro"},
			},
		},
	}

	names := extractAggregateRoleNames(ta)
	if len(names) != 0 {
		t.Errorf("expected 0 aggregate roles (nx- prefix), got %d: %v", len(names), names)
	}
}

func TestNexusTeamBindingCRDFields(t *testing.T) {
	binding := &nexusv1alpha1.NexusTeamBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-binding",
			Namespace: "nexus",
		},
		Spec: nexusv1alpha1.NexusTeamBindingSpec{
			TeamAccessRef: "kostoed-analytics-gamma",
			Members: []nexusv1alpha1.TeamMember{
				{Username: "nexus_bas@go.kostoed.ru", AccessLevel: "rw"},
				{Username: "user2@go.kostoed.ru", AccessLevel: "ro"},
			},
		},
	}

	if binding.Spec.TeamAccessRef != "kostoed-analytics-gamma" {
		t.Errorf("unexpected teamAccessRef: %s", binding.Spec.TeamAccessRef)
	}
	if len(binding.Spec.Members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(binding.Spec.Members))
	}
	if binding.Spec.Members[0].Username != "nexus_bas@go.kostoed.ru" {
		t.Errorf("unexpected username: %s", binding.Spec.Members[0].Username)
	}
	if binding.Spec.Members[0].AccessLevel != "rw" {
		t.Errorf("unexpected accessLevel: %s", binding.Spec.Members[0].AccessLevel)
	}
}
