//go:build e2e
// +build e2e

package support

import (
	"github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetDeployments(t Test, namespace *corev1.Namespace, labelSelector string) []appsv1.Deployment {
	return Deployments(t, namespace, labelSelector)(t)
}

func Deployments(t Test, namespace *corev1.Namespace, labelSelector string) func(g gomega.Gomega) []appsv1.Deployment {
	return func(g gomega.Gomega) []appsv1.Deployment {
		deployments, err := t.Client().Core().Cluster(namespace.ClusterName).AppsV1().Deployments(namespace.Name).List(t.Ctx(), metav1.ListOptions{LabelSelector: labelSelector})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return deployments.Items
	}
}
