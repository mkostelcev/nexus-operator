package controller

import "errors"

const (
	successReason = "Success"
	errorReason   = "Error"
)

var (
	errSecretKeyNotFound     = errors.New("ключ не найден в Secret")
	errEmptyPassword         = errors.New("пустой password в Secret")
	errCredentialsEmpty      = errors.New("credentials в Secret пусты")
	errSecretNotFound        = errors.New("Secret не найден")
	errKeycloakCleanupFailed = errors.New("keycloak roles cleanup failed")
	errNTANotFound           = errors.New("referenced NexusTeamAccess not found")
	errMemberSyncFailed      = errors.New("member sync failed")
	errBindingCleanupFailed  = errors.New("binding cleanup failed")
)
