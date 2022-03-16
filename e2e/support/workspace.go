//go:build e2e
// +build e2e

package support

import (
	"github.com/google/uuid"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
)

type WithWorkspace struct {
	Test
}

func (w *WithWorkspace) Do(f func(workspace *tenancyv1alpha1.Workspace)) {
	workspace := createTestWorkspace(w)
	defer deleteTestWorkspace(w, workspace)

	w.Eventually(Workspace(w, workspace.Name)).Should(
		gomega.WithTransform(
			ConditionStatus(tenancyv1alpha1.WorkspaceScheduled),
			gomega.Equal(corev1.ConditionTrue),
		))

	invokeWorkspaceTestCode(w, workspace, f)
}

func Workspace(t Test, name string) func() *tenancyv1alpha1.Workspace {
	return func() *tenancyv1alpha1.Workspace {
		c, err := t.Client().Kcp().Cluster(AdminWorkspace).TenancyV1alpha1().Workspaces().Get(t.Ctx(), name, metav1.GetOptions{})
		t.Expect(err).NotTo(gomega.HaveOccurred())
		return c
	}
}

func createTestWorkspace(t Test) *tenancyv1alpha1.Workspace {
	name := "test-" + uuid.New().String()

	workspace := &tenancyv1alpha1.Workspace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: tenancyv1alpha1.SchemeGroupVersion.String(),
			Kind:       "Workspace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: tenancyv1alpha1.WorkspaceSpec{
			InheritFrom: AdminWorkspace,
		},
	}

	workspace, err := t.Client().Kcp().Cluster(AdminWorkspace).TenancyV1alpha1().Workspaces().Create(t.Ctx(), workspace, metav1.CreateOptions{})
	if err != nil {
		t.Expect(err).NotTo(gomega.HaveOccurred())
	}

	return workspace
}

func invokeWorkspaceTestCode(t Test, workspace *tenancyv1alpha1.Workspace, do func(*tenancyv1alpha1.Workspace)) {
	defer func() {
		// nolint: staticcheck
		if t.T().Failed() {
			// TODO
		}
	}()

	do(workspace)
}

func deleteTestWorkspace(t Test, workspace *tenancyv1alpha1.Workspace) {
	propagationPolicy := metav1.DeletePropagationBackground
	err := t.Client().Kcp().Cluster(workspace.ClusterName).TenancyV1alpha1().Workspaces().Delete(t.Ctx(), workspace.Name, metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
	t.Expect(err).NotTo(gomega.HaveOccurred())
}
