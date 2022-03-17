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
	networkingv1apply "k8s.io/client-go/applyconfigurations/networking/v1"

	clusterv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/cluster/v1alpha1"
	tenancyhelper "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1/helper"

	. "github.com/kuadrant/kcp-glbc/e2e/support"
	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	kuadrantcluster "github.com/kuadrant/kcp-glbc/pkg/cluster"
)

var applyOptions = metav1.ApplyOptions{FieldManager: "kcp-glbc-e2e", Force: true}

func TestIngress(t *testing.T) {
	test := With(t)

	// Create the test workspace
	workspace := test.NewTestWorkspace()

	// Get the encoded logical cluster name for the workspace
	logicalCluster, err := tenancyhelper.EncodeLogicalClusterName(workspace)
	test.Expect(err).NotTo(HaveOccurred())

	// Register workload cluster 1 into the test workspace
	cluster1, err := NewWorkloadClusterWithKubeConfig("cluster1")
	test.Expect(err).NotTo(HaveOccurred())

	cluster1, err = test.Client().Kcp().Cluster(logicalCluster).ClusterV1alpha1().Clusters().Create(test.Ctx(), cluster1, metav1.CreateOptions{})
	test.Expect(err).NotTo(HaveOccurred())

	// Wait until cluster 1 is ready
	test.Eventually(WorkloadCluster(test, cluster1.ClusterName, cluster1.Name)).Should(WithTransform(
		ConditionStatus(clusterv1alpha1.ClusterReadyCondition),
		Equal(corev1.ConditionTrue),
	))

	// Wait until the APIs are installed
	workspaceDiscovery := test.Client().Core().Cluster(logicalCluster).Discovery()
	test.Eventually(IsAPIInstalled(workspaceDiscovery, corev1.SchemeGroupVersion.String(), "Service")).Should(BeTrue())
	test.Eventually(IsAPIInstalled(workspaceDiscovery, appsv1.SchemeGroupVersion.String(), "Deployment")).Should(BeTrue())
	test.Eventually(IsAPIInstalled(workspaceDiscovery, networkingv1.SchemeGroupVersion.String(), "Ingress")).Should(BeTrue())

	// Create a namespace with automatic scheduling disabled
	namespace := test.NewTestNamespace(InWorkspace(workspace), WithLabel("experimental.scheduling.kcp.dev/disabled", ""))

	name := "echo"

	// Create the Deployment and Service for cluster 1
	syncToCluster1 := map[string]string{ClusterLabel: cluster1.Name}

	_, err = test.Client().Core().Cluster(namespace.ClusterName).AppsV1().Deployments(namespace.Name).Create(test.Ctx(), newDeployment(name+"1", syncToCluster1), metav1.CreateOptions{})
	test.Expect(err).NotTo(HaveOccurred())

	_, err = test.Client().Core().Cluster(namespace.ClusterName).CoreV1().Services(namespace.Name).Create(test.Ctx(), newService(name+"1", syncToCluster1), metav1.CreateOptions{})
	test.Expect(err).NotTo(HaveOccurred())

	// Create the root Ingress
	_, err = test.Client().Core().Cluster(namespace.ClusterName).NetworkingV1().Ingresses(namespace.Name).Apply(test.Ctx(), ingressConfiguration(namespace.Name, name, name+"1"), applyOptions)
	test.Expect(err).NotTo(HaveOccurred())

	// Wait until the root Ingress is reconciled with the load balancer Ingresses
	test.Eventually(Ingress(test, namespace, name)).WithTimeout(TestTimeoutMedium).Should(And(
		WithTransform(Annotations, HaveKey(kuadrantcluster.ANNOTATION_HCG_HOST)),
		WithTransform(LoadBalancerIngresses, HaveLen(1)),
	))

	// Retrieve the root Ingress
	ingress := GetIngress(test, namespace, name)

	// Check a DNSRecord for the root Ingress is created with the expected Spec
	test.Eventually(DNSRecord(test, namespace, name)).Should(PointTo(MatchFields(IgnoreExtras, Fields{
		"Spec": MatchFields(IgnoreExtras,
			Fields{
				"DNSName":    Equal(ingress.Annotations[kuadrantcluster.ANNOTATION_HCG_HOST]),
				"Targets":    ConsistOf(ingress.Status.LoadBalancer.Ingress[0].IP),
				"RecordType": Equal(kuadrantv1.ARecordType),
				"RecordTTL":  Equal(int64(60)),
			}),
	})))

	// Register workload cluster 2 into the test workspace
	cluster2, err := NewWorkloadClusterWithKubeConfig("cluster2")
	test.Expect(err).NotTo(HaveOccurred())

	cluster2, err = test.Client().Kcp().Cluster(logicalCluster).ClusterV1alpha1().Clusters().Create(test.Ctx(), cluster2, metav1.CreateOptions{})
	test.Expect(err).NotTo(HaveOccurred())

	// Wait until cluster 2 is ready
	test.Eventually(WorkloadCluster(test, cluster1.ClusterName, cluster1.Name)).Should(WithTransform(
		ConditionStatus(clusterv1alpha1.ClusterReadyCondition),
		Equal(corev1.ConditionTrue),
	))

	// Create the Deployment and Service for cluster 2
	syncToCluster2 := map[string]string{ClusterLabel: cluster2.Name}

	_, err = test.Client().Core().Cluster(namespace.ClusterName).AppsV1().Deployments(namespace.Name).Create(test.Ctx(), newDeployment(name+"2", syncToCluster2), metav1.CreateOptions{})
	test.Expect(err).NotTo(HaveOccurred())

	_, err = test.Client().Core().Cluster(namespace.ClusterName).CoreV1().Services(namespace.Name).Create(test.Ctx(), newService(name+"2", syncToCluster2), metav1.CreateOptions{})
	test.Expect(err).NotTo(HaveOccurred())

	// Update the root Ingress
	_, err = test.Client().Core().Cluster(namespace.ClusterName).NetworkingV1().Ingresses(namespace.Name).Apply(test.Ctx(), ingressConfiguration(namespace.Name, name, name+"1", name+"2"), applyOptions)
	test.Expect(err).NotTo(HaveOccurred())

	// Wait until the root Ingress is reconciled with the load balancer Ingresses
	test.Eventually(Ingress(test, namespace, name)).WithTimeout(TestTimeoutMedium).Should(And(
		WithTransform(Annotations, HaveKey(kuadrantcluster.ANNOTATION_HCG_HOST)),
		WithTransform(LoadBalancerIngresses, HaveLen(2)),
	))

	// Retrieve the root Ingress
	ingress = GetIngress(test, namespace, name)

	// Check a DNSRecord for the root Ingress is updated with the expected Spec
	test.Eventually(DNSRecord(test, namespace, name)).Should(PointTo(MatchFields(IgnoreExtras, Fields{
		"Spec": MatchFields(IgnoreExtras,
			Fields{
				"DNSName": Equal(ingress.Annotations[kuadrantcluster.ANNOTATION_HCG_HOST]),
				"Targets": ConsistOf(
					ingress.Status.LoadBalancer.Ingress[0].IP,
					ingress.Status.LoadBalancer.Ingress[1].IP,
				),
				"RecordType": Equal(kuadrantv1.ARecordType),
				"RecordTTL":  Equal(int64(60)),
			}),
	})))
}

func ingressConfiguration(namespace, name string, services ...string) *networkingv1apply.IngressApplyConfiguration {
	var rules []*networkingv1apply.IngressRuleApplyConfiguration
	for _, service := range services {
		rule := networkingv1apply.IngressRule().WithHTTP(
			networkingv1apply.HTTPIngressRuleValue().WithPaths(
				networkingv1apply.HTTPIngressPath().
					WithPath("/").
					WithPathType(networkingv1.PathTypePrefix).
					WithBackend(networkingv1apply.IngressBackend().
						WithService(networkingv1apply.IngressServiceBackend().
							WithName(service).
							WithPort(networkingv1apply.ServiceBackendPort().WithName("http")),
						),
					),
			),
		)
		rules = append(rules, rule)
	}

	return networkingv1apply.Ingress(name, namespace).WithSpec(
		networkingv1apply.IngressSpec().WithRules(rules...),
	)
}

func newDeployment(name string, labels map[string]string) *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "echo-server",
							Image: "jmalloc/echo-server",
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 8080,
									Protocol:      corev1.ProtocolTCP,
								},
							},
						},
					},
				},
			},
		},
	}
}

func newService(name string, labels map[string]string) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromString("http"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}
