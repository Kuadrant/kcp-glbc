//go:build e2e

/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package support

import (
	"fmt"
	"github.com/google/uuid"
	tenancyv1beta1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1beta1"
	"github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kcp-dev/logicalcluster"
)

func InWorkspace(workspace interface{}) Option {
	switch w := workspace.(type) {
	case *tenancyv1beta1.Workspace:
		return &inWorkspace{logicalcluster.From(w).Join(w.Name)}
	case logicalcluster.Name:
		return &inWorkspace{w}
	default:
		return errorOption(func(to interface{}) error {
			return fmt.Errorf("unsupported type passed to InWorkspace option: %s", workspace)
		})
	}
}

type inWorkspace struct {
	workspace logicalcluster.Name
}

var _ Option = &inWorkspace{}

func (o *inWorkspace) applyTo(to interface{}) error {
	switch obj := to.(type) {
	case metav1.Object:
		obj.SetClusterName(o.workspace.String())

	case *workloadClusterConfig:
		obj.workspace = o.workspace

	default:
		return fmt.Errorf("cannot apply InWorkspace option to %q", to)
	}

	return nil
}

func HasImportedAPIs(t Test, workspace *tenancyv1beta1.Workspace, GVKs ...schema.GroupVersionKind) func(g gomega.Gomega) bool {
	return func(g gomega.Gomega) bool {
		// Get the logical cluster for the workspace
		logicalCluster := logicalcluster.From(workspace).Join(workspace.Name)
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

func Workspace(t Test, name string) func() *tenancyv1beta1.Workspace {
	return func() *tenancyv1beta1.Workspace {
		c, err := t.Client().Kcp().Cluster(TestOrganization).TenancyV1beta1().Workspaces().Get(t.Ctx(), name, metav1.GetOptions{})
		t.Expect(err).NotTo(gomega.HaveOccurred())
		return c
	}
}

func createTestWorkspace(t Test) *tenancyv1beta1.Workspace {
	name := "test-" + uuid.New().String()

	workspace := &tenancyv1beta1.Workspace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: tenancyv1beta1.SchemeGroupVersion.String(),
			Kind:       "Workspace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: tenancyv1beta1.WorkspaceSpec{},
	}

	workspace, err := t.Client().Kcp().Cluster(TestOrganization).TenancyV1beta1().Workspaces().Create(t.Ctx(), workspace, metav1.CreateOptions{})
	if err != nil {
		t.Expect(err).NotTo(gomega.HaveOccurred())
	}

	return workspace
}

func deleteTestWorkspace(t Test, workspace *tenancyv1beta1.Workspace) {
	propagationPolicy := metav1.DeletePropagationBackground
	err := t.Client().Kcp().Cluster(logicalcluster.From(workspace)).TenancyV1beta1().Workspaces().Delete(t.Ctx(), workspace.Name, metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
	t.Expect(err).NotTo(gomega.HaveOccurred())
}
