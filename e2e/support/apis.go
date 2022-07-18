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

	"github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apisv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/apis/v1alpha1"
	"github.com/kcp-dev/logicalcluster"
)

const ComputeServiceExportName = "kubernetes"

func WithComputeServiceExport(path logicalcluster.Name) Option {
	return &withExportReference{
		path:       path,
		exportName: ComputeServiceExportName,
	}
}

func WithExportReference(path logicalcluster.Name, exportName string) Option {
	return &withExportReference{
		path:       path,
		exportName: exportName,
	}
}

type withExportReference struct {
	path       logicalcluster.Name
	exportName string
}

func (o *withExportReference) applyTo(to interface{}) error {
	binding, ok := to.(*apisv1alpha1.APIBinding)
	if !ok {
		return fmt.Errorf("cannot apply WithExportReference option to %q", to)
	}
	binding.Spec.Reference.Workspace = &apisv1alpha1.WorkspaceExportReference{
		Path:       o.path.String(),
		ExportName: o.exportName,
	}
	return nil
}

var _ Option = &withExportReference{}

func createAPIBinding(t Test, name string, options ...Option) *apisv1alpha1.APIBinding {
	binding := &apisv1alpha1.APIBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apisv1alpha1.SchemeGroupVersion.String(),
			Kind:       "APIBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apisv1alpha1.APIBindingSpec{},
	}

	for _, option := range options {
		t.Expect(option.applyTo(binding)).To(gomega.Succeed())
	}

	t.Expect(binding.Spec.Reference.Workspace).NotTo(gomega.BeNil())

	binding, err := t.Client().Kcp().Cluster(logicalcluster.From(binding)).ApisV1alpha1().APIBindings().
		Create(t.Ctx(), binding, metav1.CreateOptions{})
	t.Expect(err).NotTo(gomega.HaveOccurred())

	return binding
}

func APIBinding(t Test, workspace, name string) func(g gomega.Gomega) *apisv1alpha1.APIBinding {
	return func(g gomega.Gomega) *apisv1alpha1.APIBinding {
		c, err := t.Client().Kcp().Cluster(logicalcluster.New(workspace)).ApisV1alpha1().APIBindings().Get(t.Ctx(), name, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return c
	}
}

func APIBindingPhase(binding *apisv1alpha1.APIBinding) apisv1alpha1.APIBindingPhaseType {
	return binding.Status.Phase
}
