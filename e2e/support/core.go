//go:build e2e
// +build e2e

package support

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	conditionsapi "github.com/kcp-dev/kcp/third_party/conditions/apis/conditions/v1alpha1"
	conditionsutil "github.com/kcp-dev/kcp/third_party/conditions/util/conditions"
)

var (
	_ Option = &withLabel{}
	_ Option = &withLabels{}
)

func WithLabel(key, value string) Option {
	return &withLabel{key, value}
}

type withLabel struct {
	key, value string
}

func (o *withLabel) applyTo(to interface{}) error {
	object, ok := to.(metav1.Object)
	if !ok {
		return fmt.Errorf("cannot apply option %q to %q", o, to)
	}
	if object.GetLabels() == nil {
		object.SetLabels(map[string]string{})
	}
	object.GetLabels()[o.key] = o.value
	return nil
}

func WithLabels(labels map[string]string) Option {
	return &withLabels{labels}
}

type withLabels struct {
	labels map[string]string
}

func (o *withLabels) applyTo(to interface{}) error {
	object, ok := to.(metav1.Object)
	if !ok {
		return fmt.Errorf("cannot apply option %q to %q", o, to)
	}
	object.SetLabels(o.labels)
	return nil
}

func Annotations(object metav1.Object) map[string]string {
	return object.GetAnnotations()
}

func Labels(object metav1.Object) map[string]string {
	return object.GetLabels()
}

func ConditionStatus(conditionType conditionsapi.ConditionType) func(getter conditionsutil.Getter) corev1.ConditionStatus {
	return func(getter conditionsutil.Getter) corev1.ConditionStatus {
		c := conditionsutil.Get(getter, conditionType)
		if c == nil {
			return corev1.ConditionUnknown
		}
		return c.Status
	}
}
