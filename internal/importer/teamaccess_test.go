package importer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseCSContentSelectorName(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantTeam   string
		wantFormat string
		wantRepo   string
		wantOK     bool
	}{
		{
			name:       "simple docker",
			input:      "cs-myteam-docker-registry",
			wantTeam:   "myteam",
			wantFormat: "docker",
			wantRepo:   "registry",
			wantOK:     true,
		},
		{
			name:       "team with dashes docker",
			input:      "cs-my-cool-team-docker-registry",
			wantTeam:   "my-cool-team",
			wantFormat: "docker",
			wantRepo:   "registry",
			wantOK:     true,
		},
		{
			name:       "maven2 format",
			input:      "cs-myteam-maven2-releases",
			wantTeam:   "myteam",
			wantFormat: "maven2",
			wantRepo:   "releases",
			wantOK:     true,
		},
		{
			name:       "raw format",
			input:      "cs-teamA-raw-myraw",
			wantTeam:   "teamA",
			wantFormat: "raw",
			wantRepo:   "myraw",
			wantOK:     true,
		},
		{
			name:       "npm format",
			input:      "cs-frontend-npm-npm-hosted",
			wantTeam:   "frontend",
			wantFormat: "npm",
			wantRepo:   "npm-hosted",
			wantOK:     true,
		},
		{
			name:       "nuget format",
			input:      "cs-dotnet-nuget-nuget-hosted",
			wantTeam:   "dotnet",
			wantFormat: "nuget",
			wantRepo:   "nuget-hosted",
			wantOK:     true,
		},
		{
			name:   "no cs prefix",
			input:  "myteam-docker-registry",
			wantOK: false,
		},
		{
			name:   "unknown format",
			input:  "cs-myteam-pypi-registry",
			wantOK: false,
		},
		{
			name:   "empty team",
			input:  "cs--docker-registry",
			wantOK: false,
		},
		{
			name:   "empty repo",
			input:  "cs-myteam-docker-",
			wantOK: false,
		},
		{
			name:   "just cs prefix",
			input:  "cs-",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			team, format, repo, ok := parseCSContentSelectorName(tt.input)
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantTeam, team)
				assert.Equal(t, tt.wantFormat, format)
				assert.Equal(t, tt.wantRepo, repo)
			}
		})
	}
}

func TestActionsToAccessLevel(t *testing.T) {
	tests := []struct {
		name    string
		actions []string
		want    string
	}{
		{
			name:    "ro",
			actions: []string{"READ", "BROWSE"},
			want:    "ro",
		},
		{
			name:    "ro reversed order",
			actions: []string{"BROWSE", "READ"},
			want:    "ro",
		},
		{
			name:    "rw",
			actions: []string{"READ", "BROWSE", "ADD", "EDIT"},
			want:    "rw",
		},
		{
			name:    "rwd",
			actions: []string{"READ", "BROWSE", "ADD", "EDIT", "DELETE"},
			want:    "rwd",
		},
		{
			name:    "unknown",
			actions: []string{"READ"},
			want:    "",
		},
		{
			name:    "empty",
			actions: []string{},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := actionsToAccessLevel(tt.actions)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractTeamPathFromCSEL(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		format     string
		want       string
	}{
		{
			name:       "docker simple",
			expression: `path =~ "^/v2/(myteam/.*)?$"`,
			format:     "docker",
			want:       "myteam",
		},
		{
			name:       "docker nested",
			expression: `path =~ "^/v2/(org/team/sub/.*)?$"`,
			format:     "docker",
			want:       "org/team/sub",
		},
		{
			name:       "maven2 simple",
			expression: `path =~ "^/ru/(myteam/.*)?$"`,
			format:     "maven2",
			want:       "myteam",
		},
		{
			name:       "maven2 nested",
			expression: `path =~ "^/ru/(ru/kostoed/team/.*)?$"`,
			format:     "maven2",
			want:       "ru/kostoed/team",
		},
		{
			name:       "raw simple",
			expression: `path =~ "^/(myteam/.*)?$"`,
			format:     "raw",
			want:       "myteam",
		},
		{
			name:       "raw nested",
			expression: `path =~ "^/(org/team/.*)?$"`,
			format:     "raw",
			want:       "org/team",
		},
		{
			name:       "npm simple",
			expression: `path =~ "^@myteam.*$"`,
			format:     "npm",
			want:       "myteam",
		},
		{
			name:       "npm nested",
			expression: `path =~ "^@org.team.sub.*$"`,
			format:     "npm",
			want:       "org/team/sub",
		},
		{
			name:       "nuget simple",
			expression: `path =~ "^myteam.*$"`,
			format:     "nuget",
			want:       "myteam",
		},
		{
			name:       "nuget nested",
			expression: `path =~ "^org.team.sub.*$"`,
			format:     "nuget",
			want:       "org/team/sub",
		},
		{
			name:       "invalid expression",
			expression: `format == "raw"`,
			format:     "docker",
			want:       "",
		},
		{
			name:       "unknown format",
			expression: `path =~ "^/myteam.*$"`,
			format:     "pypi",
			want:       "",
		},
		{
			name:       "empty expression",
			expression: "",
			format:     "docker",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTeamPathFromCSEL(tt.expression, tt.format)
			assert.Equal(t, tt.want, got)
		})
	}
}
