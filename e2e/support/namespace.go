//go:build e2e
// +build e2e

package support

import (
	"github.com/google/uuid"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func createTestNamespace(t Test, options ...Option) *corev1.Namespace {
	name := "test-" + uuid.New().String()

	namespace := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	for _, option := range options {
		t.Expect(option.applyTo(namespace)).To(gomega.Succeed())
	}

	namespace, err := t.Client().Core().Cluster(namespace.ClusterName).CoreV1().Namespaces().Create(t.Ctx(), namespace, metav1.CreateOptions{})
	t.Expect(err).NotTo(gomega.HaveOccurred())

	return namespace
}

func deleteTestNamespace(t Test, namespace *corev1.Namespace) {
	propagationPolicy := metav1.DeletePropagationBackground
	err := t.Client().Core().Cluster(namespace.ClusterName).CoreV1().Namespaces().Delete(t.Ctx(), namespace.Name, metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
	t.Expect(err).NotTo(gomega.HaveOccurred())
}
