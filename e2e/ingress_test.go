//go:build e2e
// +build e2e

package e2e

import (
	"testing"

	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	appsv1apply "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	v1apply "k8s.io/client-go/applyconfigurations/meta/v1"
	networkingv1apply "k8s.io/client-go/applyconfigurations/networking/v1"

	"github.com/kcp-dev/apimachinery/pkg/logicalcluster"
	apisv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/apis/v1alpha1"
	workloadv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/workload/v1alpha1"
	kcp "github.com/kcp-dev/kcp/pkg/reconciler/workload/namespace"

	. "github.com/kuadrant/kcp-glbc/e2e/support"
	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	kuadrantcluster "github.com/kuadrant/kcp-glbc/pkg/cluster"
)

var applyOptions = metav1.ApplyOptions{FieldManager: "kcp-glbc-e2e", Force: true}

func TestIngress(t *testing.T) {
	test := With(t)
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
	cluster1 := test.NewWorkloadCluster("kcp-cluster-1", WithKubeConfigByName, InWorkspace(workspace))

	// Wait until cluster 1 is ready
	test.Eventually(WorkloadCluster(test, cluster1.ClusterName, cluster1.Name)).Should(WithTransform(
		ConditionStatus(workloadv1alpha1.WorkloadClusterReadyCondition),
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

	// Create the root Deployment
	_, err := test.Client().Core().Cluster(logicalcluster.From(namespace)).AppsV1().Deployments(namespace.Name).
		Apply(test.Ctx(), deploymentConfiguration(namespace.Name, name), applyOptions)
	test.Expect(err).NotTo(HaveOccurred())

	// Create the root Service
	_, err = test.Client().Core().Cluster(logicalcluster.From(namespace)).CoreV1().Services(namespace.Name).
		Apply(test.Ctx(), serviceConfiguration(namespace.Name, name, map[string]string{}), applyOptions)
	test.Expect(err).NotTo(HaveOccurred())

	// Create the root Ingress
	_, err = test.Client().Core().Cluster(logicalcluster.From(namespace)).NetworkingV1().Ingresses(namespace.Name).
		Apply(test.Ctx(), ingressConfiguration(namespace.Name, name), applyOptions)
	test.Expect(err).NotTo(HaveOccurred())

	// Wait until the root Ingress is reconciled with the load balancer Ingresses
	test.Eventually(Ingress(test, namespace, name)).WithTimeout(TestTimeoutMedium).Should(And(
		WithTransform(Annotations, And(
			HaveKey(kuadrantcluster.ANNOTATION_HCG_HOST),
			HaveKey(kuadrantcluster.ANNOTATION_HCG_CUSTOM_HOST_REPLACED)),
		),
		WithTransform(LoadBalancerIngresses, HaveLen(1)),
		Satisfy(HostsEqualsToGeneratedHost),
	))

	// Retrieve the root Ingress
	ingress := GetIngress(test, namespace, name)

	// Check a DNSRecord for the root Ingress is created with the expected Spec
	test.Eventually(DNSRecord(test, namespace, name)).Should(And(
		WithTransform(DNSRecordEndpoints, HaveLen(1)),
		WithTransform(DNSRecordEndpoints, ContainElement(PointTo(MatchFields(IgnoreExtras,
			Fields{
				"DNSName":          Equal(ingress.Annotations[kuadrantcluster.ANNOTATION_HCG_HOST]),
				"Targets":          ConsistOf(ingress.Status.LoadBalancer.Ingress[0].IP),
				"RecordType":       Equal("A"),
				"RecordTTL":        Equal(kuadrantv1.TTL(60)),
				"SetIdentifier":    Equal(ingress.Status.LoadBalancer.Ingress[0].IP),
				"ProviderSpecific": ConsistOf(kuadrantv1.ProviderSpecific{{Name: "aws/weight", Value: "120"}}),
			})),
		)),
	))

	// Register workload cluster 2 into the test workspace
	cluster2 := test.NewWorkloadCluster("kcp-cluster-2", WithKubeConfigByName, InWorkspace(workspace))

	// Wait until cluster 2 is ready
	test.Eventually(WorkloadCluster(test, cluster2.ClusterName, cluster2.Name)).Should(WithTransform(
		ConditionStatus(workloadv1alpha1.WorkloadClusterReadyCondition),
		Equal(corev1.ConditionTrue),
	))
	// update the namespace with the second cluster placement
	_, err = test.Client().Core().Cluster(logicalcluster.From(namespace)).CoreV1().Namespaces().Apply(test.Ctx(), corev1apply.Namespace(namespace.Name).WithLabels(map[string]string{kcp.ClusterLabel: cluster2.Name}), applyOptions)

	test.Expect(err).NotTo(HaveOccurred())
	// Wait until the root Ingress is reconciled with the load balancer Ingresses
	test.Eventually(Ingress(test, namespace, name)).WithTimeout(TestTimeoutMedium).Should(And(
		WithTransform(Annotations, HaveKey(kuadrantcluster.ANNOTATION_HCG_HOST)),
		WithTransform(LoadBalancerIngresses, HaveLen(1)),
		WithTransform(Labels, HaveKeyWithValue(kcp.ClusterLabel, cluster2.Name)),
	))

	// Retrieve the root Ingress
	ingress = GetIngress(test, namespace, name)

	// Check a DNSRecord for the root Ingress is updated with the expected Spec
	test.Eventually(DNSRecord(test, namespace, name)).Should(And(
		WithTransform(DNSRecordEndpoints, HaveLen(1)),
		WithTransform(DNSRecordEndpoints, ContainElement(PointTo(MatchFields(IgnoreExtras,
			Fields{
				"DNSName":          Equal(ingress.Annotations[kuadrantcluster.ANNOTATION_HCG_HOST]),
				"Targets":          ConsistOf(ingress.Status.LoadBalancer.Ingress[0].IP),
				"RecordType":       Equal("A"),
				"RecordTTL":        Equal(kuadrantv1.TTL(60)),
				"SetIdentifier":    Equal(ingress.Status.LoadBalancer.Ingress[0].IP),
				"ProviderSpecific": ConsistOf(kuadrantv1.ProviderSpecific{{Name: "aws/weight", Value: "120"}}),
			})),
		)),
	))

	// Finally, delete the root resources
	test.Expect(test.Client().Core().Cluster(logicalcluster.From(namespace)).NetworkingV1().Ingresses(namespace.Name).
		Delete(test.Ctx(), name, metav1.DeleteOptions{})).
		To(Succeed())
	test.Expect(test.Client().Core().Cluster(logicalcluster.From(namespace)).CoreV1().Services(namespace.Name).
		Delete(test.Ctx(), name, metav1.DeleteOptions{})).
		To(Succeed())
	test.Expect(test.Client().Core().Cluster(logicalcluster.From(namespace)).AppsV1().Deployments(namespace.Name).
		Delete(test.Ctx(), name, metav1.DeleteOptions{})).
		To(Succeed())

}

func ingressConfiguration(namespace, name string) *networkingv1apply.IngressApplyConfiguration {
	return networkingv1apply.Ingress(name, namespace).WithSpec(
		networkingv1apply.IngressSpec().WithRules(networkingv1apply.IngressRule().
			WithHost("test.gblb.com").
			WithHTTP(networkingv1apply.HTTPIngressRuleValue().
				WithPaths(networkingv1apply.HTTPIngressPath().
					WithPath("/").
					WithPathType(networkingv1.PathTypePrefix).
					WithBackend(networkingv1apply.IngressBackend().
						WithService(networkingv1apply.IngressServiceBackend().
							WithName(name).
							WithPort(networkingv1apply.ServiceBackendPort().
								WithName("http"))))))))
}

func deploymentConfiguration(namespace, name string) *appsv1apply.DeploymentApplyConfiguration {
	return appsv1apply.Deployment(name, namespace).
		WithSpec(appsv1apply.DeploymentSpec().
			WithSelector(v1apply.LabelSelector().WithMatchLabels(map[string]string{"app": name})).
			WithTemplate(corev1apply.PodTemplateSpec().
				WithLabels(map[string]string{"app": name}).
				WithSpec(corev1apply.PodSpec().
					WithContainers(corev1apply.Container().
						WithName("echo-server").
						WithImage("jmalloc/echo-server").
						WithPorts(corev1apply.ContainerPort().
							WithName("http").
							WithContainerPort(8080).
							WithProtocol(corev1.ProtocolTCP))))))
}

func serviceConfiguration(namespace, name string, annotations map[string]string) *corev1apply.ServiceApplyConfiguration {
	return corev1apply.Service(name, namespace).
		WithAnnotations(annotations).
		WithSpec(corev1apply.ServiceSpec().
			WithSelector(map[string]string{"app": name}).
			WithPorts(corev1apply.ServicePort().
				WithName("http").
				WithPort(80).
				WithTargetPort(intstr.FromString("http")).
				WithProtocol(corev1.ProtocolTCP)))
}
