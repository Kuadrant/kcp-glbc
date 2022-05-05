//go:build e2e
// +build e2e

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

package e2e

import (
	"crypto/x509/pkix"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"

	"github.com/kcp-dev/apimachinery/pkg/logicalcluster"
	apisv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/apis/v1alpha1"
	conditionsapi "github.com/kcp-dev/kcp/third_party/conditions/apis/conditions/v1alpha1"

	. "github.com/kuadrant/kcp-glbc/e2e/support"
	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	kuadrantcluster "github.com/kuadrant/kcp-glbc/pkg/cluster"
	"github.com/kuadrant/kcp-glbc/pkg/util/env"
)

func TestTLS(t *testing.T) {
	test := With(t)
	test.T().Parallel()
	// Create the test workspace
	workspace := test.NewTestWorkspace()

	// Import the GLBC APIs
	binding := test.NewGLBCAPIBinding(InWorkspace(workspace))

	// Wait until the APIBinding is actually in bound phase
	test.Eventually(APIBinding(test, binding.ClusterName, binding.Name)).
		Should(WithTransform(APIBindingPhase, Equal(apisv1alpha1.APIBindingPhaseBound)))

	// And check the APIs are imported into the workspace
	test.Expect(HasImportedAPIs(test, workspace, kuadrantv1.SchemeGroupVersion.WithKind("DNSRecord"))(test)).
		Should(BeTrue())

	// Register workload cluster 1 into the test workspace
	cluster1 := test.NewWorkloadCluster("kcp-cluster-1", InWorkspace(workspace), WithKubeConfigByName, Syncer().ResourcesToSync(GLBCResources...))

	// Wait until cluster 1 is ready
	test.Eventually(WorkloadCluster(test, cluster1.ClusterName, cluster1.Name)).WithTimeout(time.Minute * 3).Should(WithTransform(
		ConditionStatus(conditionsapi.ReadyCondition),
		Equal(corev1.ConditionTrue),
	))

	// Wait until the APIs are imported into the workspace
	test.Eventually(HasImportedAPIs(test, workspace,
		corev1.SchemeGroupVersion.WithKind("Service"),
		appsv1.SchemeGroupVersion.WithKind("Deployment"),
		networkingv1.SchemeGroupVersion.WithKind("Ingress"),
	)).Should(BeTrue())

	// Create a namespace
	namespace := test.NewTestNamespace(InWorkspace(workspace))

	name := "echo"

	// Create the Deployment
	_, err := test.Client().Core().Cluster(logicalcluster.From(namespace)).AppsV1().Deployments(namespace.Name).
		Apply(test.Ctx(), DeploymentConfiguration(namespace.Name, name), ApplyOptions)
	test.Expect(err).NotTo(HaveOccurred())

	// Create the Service
	_, err = test.Client().Core().Cluster(logicalcluster.From(namespace)).CoreV1().Services(namespace.Name).
		Apply(test.Ctx(), ServiceConfiguration(namespace.Name, name, map[string]string{}), ApplyOptions)
	test.Expect(err).NotTo(HaveOccurred())

	// Create the Ingress
	_, err = test.Client().Core().Cluster(logicalcluster.From(namespace)).NetworkingV1().Ingresses(namespace.Name).
		Apply(test.Ctx(), IngressConfiguration(namespace.Name, name), ApplyOptions)
	test.Expect(err).NotTo(HaveOccurred())

	// Wait until the Ingress is reconciled with the load balancer Ingresses
	test.Eventually(Ingress(test, namespace, name)).WithTimeout(TestTimeoutMedium).Should(And(
		WithTransform(Annotations, And(
			HaveKey(kuadrantcluster.ANNOTATION_HCG_HOST),
			HaveKey(kuadrantcluster.ANNOTATION_HCG_CUSTOM_HOST_REPLACED)),
		),
		WithTransform(LoadBalancerIngresses, HaveLen(1)),
		Satisfy(HostsEqualsToGeneratedHost),
	))

	// Retrieve the Ingress
	ingress := GetIngress(test, namespace, name)
	hostname := ingress.Annotations[kuadrantcluster.ANNOTATION_HCG_HOST]
	context, err := kuadrantcluster.NewControlObjectMapper(ingress)
	test.Expect(err).NotTo(HaveOccurred())

	// Check the Ingress TLS spec
	test.Expect(ingress).To(WithTransform(IngressTLS, ConsistOf(
		networkingv1.IngressTLS{
			Hosts:      []string{hostname},
			SecretName: context.Name(),
		}),
	))

	// Check the TLS Secret
	issuer := env.GetEnvString("GLBC_TLS_PROVIDER", "glbc-ca")
	fields := map[string]types.GomegaMatcher{
		"DNSNames": ConsistOf(hostname),
	}
	switch issuer {

	case "glbc-ca":
		fields["Issuer"] = Equal(pkix.Name{
			Organization: []string{"Kuadrant"},
			Names: []pkix.AttributeTypeAndValue{
				{
					Type:  []int{2, 5, 4, 10},
					Value: "Kuadrant",
				},
			},
		})

	case "le-staging":
		fields["Issuer"] = Equal(pkix.Name{
			Country:      []string{"US"},
			Organization: []string{"(STAGING) Let's Encrypt"},
			CommonName:   "(STAGING) Artificial Apricot R3",
			Names: []pkix.AttributeTypeAndValue{
				{
					Type:  []int{2, 5, 4, 6},
					Value: "US",
				},
				{
					Type:  []int{2, 5, 4, 10},
					Value: "(STAGING) Let's Encrypt",
				},
				{
					Type:  []int{2, 5, 4, 3},
					Value: "(STAGING) Artificial Apricot R3",
				},
			},
		})
		fields["Subject"] = Equal(pkix.Name{
			CommonName: hostname,
			Names: []pkix.AttributeTypeAndValue{
				{
					Type:  []int{2, 5, 4, 3},
					Value: hostname,
				},
			},
		})

	case "le-production":
		fields["Issuer"] = Equal(pkix.Name{
			Country:      []string{"US"},
			Organization: []string{"Let's Encrypt"},
			CommonName:   "Artificial Apricot R3",
			Names: []pkix.AttributeTypeAndValue{
				{
					Type:  []int{2, 5, 4, 6},
					Value: "US",
				},
				{
					Type:  []int{2, 5, 4, 10},
					Value: "Let's Encrypt",
				},
				{
					Type:  []int{2, 5, 4, 3},
					Value: "R3",
				},
			},
		})
		fields["Subject"] = Equal(pkix.Name{
			CommonName: hostname,
			Names: []pkix.AttributeTypeAndValue{
				{
					Type:  []int{2, 5, 4, 3},
					Value: hostname,
				},
			},
		})
	}

	test.Eventually(Secret(test, namespace, context.Name())).
		WithTimeout(TestTimeoutMedium).
		Should(WithTransform(Certificate, PointTo(MatchFields(IgnoreExtras, fields))))
}
