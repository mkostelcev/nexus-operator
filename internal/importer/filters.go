package importer

import (
	"strings"

	"github.com/mkostelcev/nexus-operator/pkg/nexus"
)

// isBuiltinUser проверяет, является ли пользователь встроенным (readOnly, admin, anonymous).
func isBuiltinUser(user nexus.User) bool {
	if user.ReadOnly {
		return true
	}
	if user.UserId == "admin" || user.UserId == "anonymous" {
		return true
	}
	return false
}

// isBuiltinRole проверяет, является ли роль встроенной (readOnly, не-default source,
// или автоматически созданной Keycloak realm'ом — с префиксом "ClientRole:").
func isBuiltinRole(role nexus.Role) bool {
	if role.ReadOnly {
		return true
	}
	if role.Source != "" && role.Source != "default" {
		return true
	}
	if strings.HasPrefix(role.ID, "ClientRole:") {
		return true
	}
	return false
}

// isBuiltinPrivilege проверяет, является ли привилегия встроенной.
func isBuiltinPrivilege(priv nexus.PrivilegeListItem) bool {
	return priv.ReadOnly
}

// isSupportedPrivilegeType проверяет, поддерживается ли тип привилегии оператором.
func isSupportedPrivilegeType(t string) bool {
	switch t {
	case nexus.PrivilegeTypeWildcard,
		nexus.PrivilegeTypeApplication,
		nexus.PrivilegeTypeRepositoryView,
		nexus.PrivilegeTypeRepositoryAdmin,
		nexus.PrivilegeTypeRepositoryContentSelector:
		return true
	}
	return false
}

// supportedRepoTypes — множество поддерживаемых оператором типов репозиториев.
var supportedRepoTypes = map[string]bool{
	nexus.TypeMavenHosted:      true,
	nexus.TypeMavenProxy:       true,
	nexus.TypeMavenGroup:       true,
	nexus.TypeNpmHosted:        true,
	nexus.TypeNpmProxy:         true,
	nexus.TypeNpmGroup:         true,
	nexus.TypeDockerHosted:     true,
	nexus.TypeDockerProxy:      true,
	nexus.TypeDockerGroup:      true,
	nexus.TypeRawHosted:        true,
	nexus.TypeRawProxy:         true,
	nexus.TypeRawGroup:         true,
	nexus.TypeHelmHosted:       true,
	nexus.TypeHelmProxy:        true,
	nexus.TypePypiHosted:       true,
	nexus.TypePypiProxy:        true,
	nexus.TypePypiGroup:        true,
	nexus.TypeNugetHosted:      true,
	nexus.TypeNugetProxy:       true,
	nexus.TypeNugetGroup:       true,
	nexus.TypeAptHosted:        true,
	nexus.TypeAptProxy:         true,
	nexus.TypeCargoGroup:       true,
	nexus.TypeCargoHosted:      true,
	nexus.TypeCargoProxy:       true,
	nexus.TypeCocoapodsProxy:   true,
	nexus.TypeComposerProxy:    true,
	nexus.TypeConanGroup:       true,
	nexus.TypeConanHosted:      true,
	nexus.TypeConanProxy:       true,
	nexus.TypeCondaProxy:       true,
	nexus.TypeGitlfsHosted:     true,
	nexus.TypeGoGroup:          true,
	nexus.TypeGoProxy:          true,
	nexus.TypeHuggingfaceProxy: true,
	nexus.TypeP2Proxy:          true,
	nexus.TypeRGroup:           true,
	nexus.TypeRHosted:          true,
	nexus.TypeRProxy:           true,
	nexus.TypeRubygemsGroup:    true,
	nexus.TypeRubygemsHosted:   true,
	nexus.TypeRubygemsProxy:    true,
	nexus.TypeYumGroup:         true,
	nexus.TypeYumHosted:        true,
	nexus.TypeYumProxy:         true,
}

// nexusFormatTypeToOperatorType конвертирует format+type из Nexus API в тип оператора.
// Nexus API возвращает format "maven2", а оператор использует "maven-hosted" и т.д.
func nexusFormatTypeToOperatorType(format, repoType string) (string, bool) {
	// Nexus возвращает "maven2" как format, оператор использует "maven"
	prefix := format
	if format == "maven2" {
		prefix = "maven"
	}
	operatorType := prefix + "-" + repoType
	if supportedRepoTypes[operatorType] {
		return operatorType, true
	}
	return "", false
}
