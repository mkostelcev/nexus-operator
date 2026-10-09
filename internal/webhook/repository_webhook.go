package webhook

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
)

// RepositoryValidator валидирует Repository CR.
type RepositoryValidator struct{}

var _ admission.CustomValidator = &RepositoryValidator{}

var (
	errExpectedRepository     = errors.New("ожидался Repository")
	errProxyRequired          = errors.New("spec.proxy обязателен")
	errProxyRemoteUrlRequired = errors.New("spec.proxy.remoteUrl обязателен")
	errGroupRequired          = errors.New("spec.group обязателен")
	errGroupMemberNamesEmpty  = errors.New("spec.group.memberNames не может быть пустым")
	errProxyMustBeEmpty       = errors.New("spec.proxy должен быть пустым для hosted-типа")
	errGroupMustBeEmpty       = errors.New("spec.group должен быть пустым для hosted-типа")
	errMavenRequired          = errors.New("spec.maven обязателен")
	errDockerRequired         = errors.New("spec.docker обязателен")
	errYumRequired            = errors.New("spec.yum обязателен")
)

func (v *RepositoryValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	repo, ok := obj.(*nexusv1alpha1.Repository)
	if !ok {
		return nil, fmt.Errorf("%w: получен %T", errExpectedRepository, obj)
	}
	return nil, validateRepository(repo)
}

func (v *RepositoryValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	repo, ok := newObj.(*nexusv1alpha1.Repository)
	if !ok {
		return nil, fmt.Errorf("%w: получен %T", errExpectedRepository, newObj)
	}
	return nil, validateRepository(repo)
}

func (v *RepositoryValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func validateRepository(repo *nexusv1alpha1.Repository) error {
	repoType := repo.Spec.Type

	if isProxyType(repoType) {
		if repo.Spec.Proxy == nil {
			return fmt.Errorf("%w для типа %s", errProxyRequired, repoType)
		}
		if repo.Spec.Proxy.RemoteUrl == "" {
			return fmt.Errorf("%w для типа %s", errProxyRemoteUrlRequired, repoType)
		}
	}

	if isGroupType(repoType) {
		if repo.Spec.Group == nil {
			return fmt.Errorf("%w для типа %s", errGroupRequired, repoType)
		}
		if len(repo.Spec.Group.MemberNames) == 0 {
			return fmt.Errorf("%w для типа %s", errGroupMemberNamesEmpty, repoType)
		}
	}

	if isHostedType(repoType) {
		if repo.Spec.Proxy != nil {
			return fmt.Errorf("%w %s", errProxyMustBeEmpty, repoType)
		}
		if repo.Spec.Group != nil {
			return fmt.Errorf("%w %s", errGroupMustBeEmpty, repoType)
		}
	}

	if isMavenType(repoType) && !isGroupType(repoType) && repo.Spec.Maven == nil {
		return fmt.Errorf("%w для типа %s", errMavenRequired, repoType)
	}

	if isDockerType(repoType) && repo.Spec.Docker == nil {
		return fmt.Errorf("%w для типа %s", errDockerRequired, repoType)
	}

	if repoType == "yum-hosted" {
		if repo.Spec.Yum == nil {
			return fmt.Errorf("%w для типа %s", errYumRequired, repoType)
		}
	}

	return nil
}

func isProxyType(t string) bool {
	return strings.HasSuffix(t, "-proxy")
}

func isGroupType(t string) bool {
	return strings.HasSuffix(t, "-group")
}

func isHostedType(t string) bool {
	return strings.HasSuffix(t, "-hosted")
}

func isMavenType(t string) bool {
	return strings.HasPrefix(t, "maven-")
}

func isDockerType(t string) bool {
	return strings.HasPrefix(t, "docker-")
}
