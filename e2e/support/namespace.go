//go:build e2e
// +build e2e

package support

import (
	"github.com/google/uuid"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
	tenancyhelper "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1/helper"
)

type NamespaceOption interface {
	ApplyTo(namespace *corev1.Namespace) error
}

type inWorkspace struct {
	workspace *tenancyv1alpha1.Workspace
}

func (o *inWorkspace) ApplyTo(namespace *corev1.Namespace) (err error) {
	namespace.ClusterName, err = tenancyhelper.EncodeLogicalClusterName(o.workspace)
	return
}

func InWorkspace(workspace *tenancyv1alpha1.Workspace) NamespaceOption {
	return &inWorkspace{workspace}
}

type withLabel struct {
	key, value string
}

func (o *withLabel) ApplyTo(namespace *corev1.Namespace) error {
	if namespace.Labels == nil {
		namespace.Labels = map[string]string{}
	}
	namespace.Labels[o.key] = o.value
	return nil
}

func WithLabel(key, value string) NamespaceOption {
	return &withLabel{key, value}
}

type withLabels struct {
	labels map[string]string
}

func (o *withLabels) ApplyTo(namespace *corev1.Namespace) error {
	namespace.Labels = o.labels
	return nil
}

func WithLabels(labels map[string]string) NamespaceOption {
	return &withLabels{labels}
}

func createTestNamespace(t Test, options ...NamespaceOption) *corev1.Namespace {
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
		t.Expect(option.ApplyTo(namespace)).To(gomega.Succeed())
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
