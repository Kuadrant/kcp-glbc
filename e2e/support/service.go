//go:build e2e
// +build e2e

package support

import (
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetServices(t Test, namespace *corev1.Namespace, labelSelector string) []corev1.Service {
	return Services(t, namespace, labelSelector)(t)
}

func Services(t Test, namespace *corev1.Namespace, labelSelector string) func(g gomega.Gomega) []corev1.Service {
	return func(g gomega.Gomega) []corev1.Service {
		services, err := t.Client().Core().Cluster(namespace.ClusterName).CoreV1().Services(namespace.Name).List(t.Ctx(), metav1.ListOptions{LabelSelector: labelSelector})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return services.Items
	}
}
