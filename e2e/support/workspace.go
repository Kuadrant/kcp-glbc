//go:build e2e
// +build e2e

package support

import (
	"github.com/google/uuid"
	"github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
	tenancyhelper "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1/helper"
)

func InWorkspace(workspace *tenancyv1alpha1.Workspace) Option {
	return &inWorkspace{workspace}
}

type inWorkspace struct {
	workspace *tenancyv1alpha1.Workspace
}

func (o *inWorkspace) applyTo(object metav1.Object) error {
	clusterName, err := tenancyhelper.EncodeLogicalClusterName(o.workspace)
	if err != nil {
		return err
	}
	object.SetClusterName(clusterName)
	return nil
}

func HasImportedAPIs(t Test, workspace *tenancyv1alpha1.Workspace, GVKs ...schema.GroupVersionKind) func(g gomega.Gomega) bool {
	return func(g gomega.Gomega) bool {
		// Get the encoded logical cluster name for the workspace
		logicalCluster, err := tenancyhelper.EncodeLogicalClusterName(workspace)
		t.Expect(err).NotTo(gomega.HaveOccurred())

		discovery := t.Client().Core().Cluster(logicalCluster).Discovery()

	GVKs:
		for _, GKV := range GVKs {
			resources, err := discovery.ServerResourcesForGroupVersion(GKV.GroupVersion().String())
			if err != nil {
				if errors.IsNotFound(err) {
					return false
				}
				g.Expect(err).NotTo(gomega.HaveOccurred())
			}
			for _, resource := range resources.APIResources {
				if resource.Kind == GKV.Kind {
					continue GVKs
				}
			}
			return false
		}

		return true
	}
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

func deleteTestWorkspace(t Test, workspace *tenancyv1alpha1.Workspace) {
	propagationPolicy := metav1.DeletePropagationBackground
	err := t.Client().Kcp().Cluster(workspace.ClusterName).TenancyV1alpha1().Workspaces().Delete(t.Ctx(), workspace.Name, metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
	t.Expect(err).NotTo(gomega.HaveOccurred())
}
