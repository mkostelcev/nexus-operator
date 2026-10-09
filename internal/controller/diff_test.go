package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiffPaths(t *testing.T) {
	desired := map[string]interface{}{
		"online": true,
		"httpClient": map[string]interface{}{
			"authentication": map[string]interface{}{
				"username": "user",
				"password": "supersecret",
			},
		},
		"group": map[string]interface{}{
			"memberNames": []interface{}{"a", "b"},
		},
	}
	current := map[string]interface{}{
		"online": true,
		"httpClient": map[string]interface{}{
			"authentication": map[string]interface{}{
				"username": "user",
			},
		},
		"group": map[string]interface{}{
			"memberNames": []interface{}{"a", "c"},
		},
	}

	t.Run("returns paths of differing fields", func(t *testing.T) {
		paths := diffPaths(desired, current)
		assert.ElementsMatch(t, []string{
			"httpClient.authentication.password",
			"group.memberNames[1]",
		}, paths)
	})

	t.Run("values never leak into paths", func(t *testing.T) {
		for _, p := range diffPaths(desired, current) {
			assert.NotContains(t, p, "supersecret")
		}
	})

	t.Run("equal inputs produce empty diff", func(t *testing.T) {
		assert.Empty(t, diffPaths(desired, desired))
	})
}
