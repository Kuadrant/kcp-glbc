//go:build e2e || performance

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

package performance

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"

	"github.com/kuadrant/kcp-glbc/pkg/util/env"
)

const (
	testOrganization = "TEST_ORGANISATION"
	testWorkspace = "TEST_WORKSPACE"
	testDNSRecordCount = "TEST_DNSRECORD_COUNT"
	testIngressCount = "TEST_INGRESS_COUNT"

	defaultTestDNSRecordCount = 1
	defaultTestIngressCount = 1
)

func getTestWorkspace() *tenancyv1alpha1.ClusterWorkspace {

	org := tenancyv1alpha1.RootCluster.Join(env.GetEnvString(testOrganization, "default"))
	name := env.GetEnvString(testWorkspace, "kcp-glbc-user")

	return &tenancyv1alpha1.ClusterWorkspace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: tenancyv1alpha1.SchemeGroupVersion.String(),
			Kind:       "Workspace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			ClusterName: org.String(),
		},
		Spec: tenancyv1alpha1.ClusterWorkspaceSpec{},
	}
}
