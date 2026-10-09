package importer

import (
	"testing"

	"github.com/mkostelcev/nexus-operator/pkg/nexus"
	"github.com/stretchr/testify/assert"
)

func Test_isBuiltinUser(t *testing.T) {
	tests := []struct {
		name string
		user nexus.User
		want bool
	}{
		{
			name: "builtin user - ReadOnly true",
			user: nexus.User{
				UserId:   "someuser",
				ReadOnly: true,
			},
			want: true,
		},
		{
			name: "builtin user - admin",
			user: nexus.User{
				UserId:   "admin",
				ReadOnly: false,
			},
			want: true,
		},
		{
			name: "builtin user - anonymous",
			user: nexus.User{
				UserId:   "anonymous",
				ReadOnly: false,
			},
			want: true,
		},
		{
			name: "builtin user - ReadOnly and admin",
			user: nexus.User{
				UserId:   "admin",
				ReadOnly: true,
			},
			want: true,
		},
		{
			name: "custom user - not builtin",
			user: nexus.User{
				UserId:   "customuser",
				ReadOnly: false,
			},
			want: false,
		},
		{
			name: "custom user - case sensitive userId",
			user: nexus.User{
				UserId:   "Admin",
				ReadOnly: false,
			},
			want: false,
		},
		{
			name: "custom user - case sensitive anonymous",
			user: nexus.User{
				UserId:   "Anonymous",
				ReadOnly: false,
			},
			want: false,
		},
		{
			name: "empty userId - not builtin",
			user: nexus.User{
				UserId:   "",
				ReadOnly: false,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBuiltinUser(tt.user)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_isBuiltinRole(t *testing.T) {
	tests := []struct {
		name string
		role nexus.Role
		want bool
	}{
		{
			name: "builtin role - ReadOnly true",
			role: nexus.Role{
				ID:       "custom-role",
				ReadOnly: true,
				Source:   "default",
			},
			want: true,
		},
		{
			name: "builtin role - Source not default and not empty",
			role: nexus.Role{
				ID:       "ldap-role",
				ReadOnly: false,
				Source:   "ldap",
			},
			want: true,
		},
		{
			name: "builtin role - Source empty but ReadOnly",
			role: nexus.Role{
				ID:       "readonly-role",
				ReadOnly: true,
				Source:   "",
			},
			want: true,
		},
		{
			name: "custom role - default source and not readonly",
			role: nexus.Role{
				ID:       "my-role",
				ReadOnly: false,
				Source:   "default",
			},
			want: false,
		},
		{
			name: "custom role - empty source and not readonly",
			role: nexus.Role{
				ID:       "another-role",
				ReadOnly: false,
				Source:   "",
			},
			want: false,
		},
		{
			name: "builtin role - both ReadOnly and non-default source",
			role: nexus.Role{
				ID:       "system-role",
				ReadOnly: true,
				Source:   "system",
			},
			want: true,
		},
		{
			name: "builtin role - external source",
			role: nexus.Role{
				ID:       "external-role",
				ReadOnly: false,
				Source:   "external",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBuiltinRole(tt.role)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_isBuiltinPrivilege(t *testing.T) {
	tests := []struct {
		name string
		priv nexus.PrivilegeListItem
		want bool
	}{
		{
			name: "builtin privilege - ReadOnly true",
			priv: nexus.PrivilegeListItem{
				Name:     "nx-all",
				ReadOnly: true,
			},
			want: true,
		},
		{
			name: "custom privilege - ReadOnly false",
			priv: nexus.PrivilegeListItem{
				Name:     "custom-priv",
				ReadOnly: false,
			},
			want: false,
		},
		{
			name: "builtin privilege - empty name",
			priv: nexus.PrivilegeListItem{
				Name:     "",
				ReadOnly: true,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBuiltinPrivilege(tt.priv)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_isSupportedPrivilegeType(t *testing.T) {
	tests := []struct {
		name     string
		privType string
		want     bool
	}{
		{
			name:     "supported type - wildcard",
			privType: "wildcard",
			want:     true,
		},
		{
			name:     "supported type - application",
			privType: "application",
			want:     true,
		},
		{
			name:     "supported type - repository-view",
			privType: "repository-view",
			want:     true,
		},
		{
			name:     "supported type - repository-admin",
			privType: "repository-admin",
			want:     true,
		},
		{
			name:     "supported type - repository-content-selector",
			privType: "repository-content-selector",
			want:     true,
		},
		{
			name:     "unsupported type - script",
			privType: "script",
			want:     false,
		},
		{
			name:     "unsupported type - empty",
			privType: "",
			want:     false,
		},
		{
			name:     "unsupported type - random",
			privType: "random-type",
			want:     false,
		},
		{
			name:     "unsupported type - case sensitive",
			privType: "Wildcard",
			want:     false,
		},
		{
			name:     "unsupported type - repository",
			privType: "repository",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSupportedPrivilegeType(tt.privType)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_nexusFormatTypeToOperatorType(t *testing.T) {
	tests := []struct {
		name     string
		format   string
		repoType string
		wantType string
		wantOk   bool
	}{
		{
			name:     "maven hosted",
			format:   "maven2",
			repoType: "hosted",
			wantType: "maven-hosted",
			wantOk:   true,
		},
		{
			name:     "maven proxy",
			format:   "maven2",
			repoType: "proxy",
			wantType: "maven-proxy",
			wantOk:   true,
		},
		{
			name:     "maven group",
			format:   "maven2",
			repoType: "group",
			wantType: "maven-group",
			wantOk:   true,
		},
		{
			name:     "docker hosted",
			format:   "docker",
			repoType: "hosted",
			wantType: "docker-hosted",
			wantOk:   true,
		},
		{
			name:     "docker proxy",
			format:   "docker",
			repoType: "proxy",
			wantType: "docker-proxy",
			wantOk:   true,
		},
		{
			name:     "docker group",
			format:   "docker",
			repoType: "group",
			wantType: "docker-group",
			wantOk:   true,
		},
		{
			name:     "npm hosted",
			format:   "npm",
			repoType: "hosted",
			wantType: "npm-hosted",
			wantOk:   true,
		},
		{
			name:     "npm proxy",
			format:   "npm",
			repoType: "proxy",
			wantType: "npm-proxy",
			wantOk:   true,
		},
		{
			name:     "npm group",
			format:   "npm",
			repoType: "group",
			wantType: "npm-group",
			wantOk:   true,
		},
		{
			name:     "raw hosted",
			format:   "raw",
			repoType: "hosted",
			wantType: "raw-hosted",
			wantOk:   true,
		},
		{
			name:     "raw proxy",
			format:   "raw",
			repoType: "proxy",
			wantType: "raw-proxy",
			wantOk:   true,
		},
		{
			name:     "raw group",
			format:   "raw",
			repoType: "group",
			wantType: "raw-group",
			wantOk:   true,
		},
		{
			name:     "unsupported format - bower",
			format:   "bower",
			repoType: "hosted",
			wantType: "",
			wantOk:   false,
		},
		{
			name:     "unsupported type - virtual",
			format:   "maven2",
			repoType: "virtual",
			wantType: "",
			wantOk:   false,
		},
		{
			name:     "empty format",
			format:   "",
			repoType: "hosted",
			wantType: "",
			wantOk:   false,
		},
		{
			name:     "empty type",
			format:   "maven2",
			repoType: "",
			wantType: "",
			wantOk:   false,
		},
		{
			name:     "both empty",
			format:   "",
			repoType: "",
			wantType: "",
			wantOk:   false,
		},
		{
			name:     "case sensitive format",
			format:   "Maven2",
			repoType: "hosted",
			wantType: "",
			wantOk:   false,
		},
		{
			name:     "case sensitive type",
			format:   "maven2",
			repoType: "Hosted",
			wantType: "",
			wantOk:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotOk := nexusFormatTypeToOperatorType(tt.format, tt.repoType)
			assert.Equal(t, tt.wantType, gotType)
			assert.Equal(t, tt.wantOk, gotOk)
		})
	}
}
