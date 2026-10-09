package webhook

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
)

// RoutingRuleValidator валидирует RoutingRule CR.
type RoutingRuleValidator struct{}

var _ admission.CustomValidator = &RoutingRuleValidator{}

var (
	errExpectedRoutingRule = errors.New("ожидался RoutingRule")
	errInvalidMode         = errors.New("spec.mode должен быть BLOCK или ALLOW")
	errMatchersRequired    = errors.New("spec.matchers должен содержать минимум 1 элемент")
	errMatcherBlank        = errors.New("пустой matcher не допускается")
)

func (v *RoutingRuleValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	rr, ok := obj.(*nexusv1alpha1.RoutingRule)
	if !ok {
		return nil, fmt.Errorf("%w: получен %T", errExpectedRoutingRule, obj)
	}
	return validateRoutingRule(rr)
}

func (v *RoutingRuleValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	rr, ok := newObj.(*nexusv1alpha1.RoutingRule)
	if !ok {
		return nil, fmt.Errorf("%w: получен %T", errExpectedRoutingRule, newObj)
	}
	return validateRoutingRule(rr)
}

func (v *RoutingRuleValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func validateRoutingRule(rr *nexusv1alpha1.RoutingRule) (admission.Warnings, error) {
	if rr.Spec.Mode != "BLOCK" && rr.Spec.Mode != "ALLOW" {
		return nil, fmt.Errorf("%w: получено %s", errInvalidMode, rr.Spec.Mode)
	}

	if len(rr.Spec.Matchers) == 0 {
		return nil, errMatchersRequired
	}

	// Nexus компилирует matchers через java.util.regex.Pattern. RE2 разбирает не всё,
	// что принимает Java (lookaround, possessive, \p{javaLowerCase}, \R и другое),
	// поэтому ошибка regexp.Compile только предупреждение: окончательно проверит Nexus
	var warnings admission.Warnings
	for i, matcher := range rr.Spec.Matchers {
		if strings.TrimSpace(matcher) == "" {
			return nil, fmt.Errorf("spec.matchers[%d]: %w", i, errMatcherBlank)
		}
		if _, err := regexp.Compile(matcher); err != nil {
			warnings = append(warnings, fmt.Sprintf(
				"spec.matchers[%d] не разобран как RE2 (%v), синтаксис Java проверит Nexus", i, err))
		}
	}

	return warnings, nil
}
