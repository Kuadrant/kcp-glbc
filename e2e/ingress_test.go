//go:build e2e
// +build e2e

package e2e

import (
	"testing"

	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	clusterv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/cluster/v1alpha1"
	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
	tenancyhelper "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1/helper"

	. "github.com/kuadrant/kcp-glbc/e2e/support"
)

func TestIngress(t *testing.T) {
	test := With(t)

	test.WithNewTestWorkspace().
		Do(func(workspace *tenancyv1alpha1.Workspace) {
			cluster, err := NewWorkloadClusterWithKubeConfig("cluster1")
			test.Expect(err).NotTo(HaveOccurred())

			logicalCluster, err := tenancyhelper.EncodeLogicalClusterName(workspace)
			test.Expect(err).NotTo(HaveOccurred())

			cluster, err = test.Client().Kcp().Cluster(logicalCluster).ClusterV1alpha1().Clusters().Create(test.Ctx(), cluster, metav1.CreateOptions{})
			test.Expect(err).NotTo(HaveOccurred())

			test.Eventually(WorkloadCluster(test, cluster.ClusterName, cluster.Name)).Should(WithTransform(
				ConditionStatus(clusterv1alpha1.ClusterReadyCondition),
				Equal(corev1.ConditionTrue),
			))

			// Wait until the APIs are installed
			workspaceDiscovery := test.Client().Core().Cluster(cluster.ClusterName).Discovery()
			test.Eventually(IsAPIInstalled(workspaceDiscovery, corev1.SchemeGroupVersion.String(), "Service")).Should(BeTrue())
			test.Eventually(IsAPIInstalled(workspaceDiscovery, appsv1.SchemeGroupVersion.String(), "Deployment")).Should(BeTrue())
			test.Eventually(IsAPIInstalled(workspaceDiscovery, networkingv1.SchemeGroupVersion.String(), "Ingress")).Should(BeTrue())

			test.WithNewTestNamespace(InWorkspace(workspace), WithLabel("experimental.scheduling.kcp.dev/disabled", "")).
				Do(func(namespace *corev1.Namespace) {
					name := "echo"
					labels := map[string]string{ClusterLabel: cluster.Name}

					deployment := newDeployment(name, labels)
					deployment, err = test.Client().Core().Cluster(namespace.ClusterName).AppsV1().Deployments(namespace.Name).Create(test.Ctx(), deployment, metav1.CreateOptions{})
					test.Expect(err).NotTo(HaveOccurred())

					service := newService(name, labels)
					service, err = test.Client().Core().Cluster(namespace.ClusterName).CoreV1().Services(namespace.Name).Create(test.Ctx(), service, metav1.CreateOptions{})
					test.Expect(err).NotTo(HaveOccurred())

					ingress := newIngress(name)
					ingress, err = test.Client().Core().Cluster(namespace.ClusterName).NetworkingV1().Ingresses(namespace.Name).Create(test.Ctx(), ingress, metav1.CreateOptions{})
					test.Expect(err).NotTo(HaveOccurred())

					test.Eventually(Ingress(test, namespace, name)).WithTimeout(TestTimeoutMedium).Should(WithTransform(LoadBalancerIngresses, HaveLen(1)))
				})
		})
}

func newIngress(name string) *networkingv1.Ingress {
	return &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{
			APIVersion: networkingv1.SchemeGroupVersion.String(),
			Kind:       "Ingress",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: PathTypeP(networkingv1.PathTypePrefix),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "echo",
											Port: networkingv1.ServiceBackendPort{
												Name: "http",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
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
					"app": "echo-server",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "echo-server",
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
				"app": "echo-server",
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
