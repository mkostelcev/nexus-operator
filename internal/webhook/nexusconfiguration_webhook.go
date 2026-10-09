package webhook

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
)

// NexusConfigurationValidator валидирует NexusConfiguration CR.
type NexusConfigurationValidator struct{}

var _ admission.CustomValidator = &NexusConfigurationValidator{}

var (
	errExpectedNexusConfiguration = errors.New("ожидался NexusConfiguration")
	errProxyHostRequired          = errors.New("host обязателен при enabled=true")
	errProxyPortInvalid           = errors.New("port должен быть числом в диапазоне 1-65535")
	errRealmsActiveEmpty          = errors.New("securityRealms.active не может быть пустым")
)

func (v *NexusConfigurationValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	resource, ok := obj.(*nexusv1alpha1.NexusConfiguration)
	if !ok {
		return nil, fmt.Errorf("%w: получен %T", errExpectedNexusConfiguration, obj)
	}
	return nil, validateNexusConfiguration(resource)
}

func (v *NexusConfigurationValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	resource, ok := newObj.(*nexusv1alpha1.NexusConfiguration)
	if !ok {
		return nil, fmt.Errorf("%w: получен %T", errExpectedNexusConfiguration, newObj)
	}
	return nil, validateNexusConfiguration(resource)
}

func (v *NexusConfigurationValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func validateNexusConfiguration(cr *nexusv1alpha1.NexusConfiguration) error {
	if cr.Spec.HttpProxy != nil {
		if err := validateProxyServer("spec.httpProxy.httpProxy", cr.Spec.HttpProxy.HttpProxy); err != nil {
			return err
		}
		if err := validateProxyServer("spec.httpProxy.httpsProxy", cr.Spec.HttpProxy.HttpsProxy); err != nil {
			return err
		}
	}

	if cr.Spec.SecurityRealms != nil {
		if len(cr.Spec.SecurityRealms.Active) == 0 {
			return errRealmsActiveEmpty
		}
	}

	return nil
}

func validateProxyServer(path string, proxy *nexusv1alpha1.HttpProxyServerConfig) error {
	if proxy == nil {
		return nil
	}

	if proxy.Enabled && proxy.Host == "" {
		return fmt.Errorf("%s: %w", path, errProxyHostRequired)
	}

	port, err := strconv.Atoi(proxy.Port)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("%s: %w: получено %q", path, errProxyPortInvalid, proxy.Port)
	}

	return nil
}
