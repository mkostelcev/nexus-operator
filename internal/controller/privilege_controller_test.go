package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrivilegeNeedsUpdate_ExtraFieldsInCurrent(t *testing.T) {
	r := &PrivilegeReconciler{}

	current := map[string]interface{}{
		"name":        "repo-view-priv",
		"type":        "repository-view",
		"description": "View repo",
		"repository":  "maven-central",
		"format":      "maven2",
		"actions":     []interface{}{"READ", "BROWSE"},
		"readOnly":    false,
		"id":          "some-internal-id",
		"source":      "default",
	}

	desired := map[string]interface{}{
		"name":        "repo-view-priv",
		"type":        "repository-view",
		"description": "View repo",
		"repository":  "maven-central",
		"format":      "maven2",
		"actions":     []interface{}{"READ", "BROWSE"},
	}

	assert.Empty(t, r.configDiff(current, desired))
}

func TestPrivilegeNeedsUpdate_DescriptionChanged(t *testing.T) {
	r := &PrivilegeReconciler{}

	current := map[string]interface{}{
		"name":        "repo-view-priv",
		"type":        "repository-view",
		"description": "Old description",
		"repository":  "maven-central",
		"format":      "maven2",
		"actions":     []interface{}{"READ"},
		"readOnly":    false,
	}

	desired := map[string]interface{}{
		"name":        "repo-view-priv",
		"type":        "repository-view",
		"description": "New description",
		"repository":  "maven-central",
		"format":      "maven2",
		"actions":     []interface{}{"READ"},
	}

	assert.NotEmpty(t, r.configDiff(current, desired))
}

func TestPrivilegeNeedsUpdate_SameConfigs(t *testing.T) {
	r := &PrivilegeReconciler{}

	current := map[string]interface{}{
		"name":        "nx-all",
		"type":        "wildcard",
		"description": "All permissions",
		"pattern":     "nexus:*",
		"readOnly":    false,
	}

	desired := map[string]interface{}{
		"name":        "nx-all",
		"type":        "wildcard",
		"description": "All permissions",
		"pattern":     "nexus:*",
	}

	assert.Empty(t, r.configDiff(current, desired))
}
