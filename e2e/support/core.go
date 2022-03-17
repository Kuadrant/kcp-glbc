//go:build e2e
// +build e2e

package support

import (
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"

	conditionsapi "github.com/kcp-dev/kcp/third_party/conditions/apis/conditions/v1alpha1"
	conditionsutil "github.com/kcp-dev/kcp/third_party/conditions/util/conditions"
)

func Annotations(object metav1.Object) map[string]string {
	return object.GetAnnotations()
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

func IsAPIInstalled(c discovery.DiscoveryInterface, groupVersion string, kind string) func(g gomega.Gomega) bool {
	return func(g gomega.Gomega) bool {
		resources, err := c.ServerResourcesForGroupVersion(groupVersion)
		if err != nil {
			if errors.IsNotFound(err) {
				return false
			}
			g.Expect(err).NotTo(gomega.HaveOccurred())
		}

		for _, resource := range resources.APIResources {
			if resource.Kind == kind {
				return true
			}
		}

		return false
	}
}
