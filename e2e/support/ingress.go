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

package support

import (
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kcp-dev/apimachinery/pkg/logicalcluster"

	kuadrantcluster "github.com/kuadrant/kcp-glbc/pkg/cluster"
)

func GetIngress(t Test, namespace *corev1.Namespace, name string) *networkingv1.Ingress {
	t.T().Helper()
	return Ingress(t, namespace, name)(t)
}

func Ingress(t Test, namespace *corev1.Namespace, name string) func(g gomega.Gomega) *networkingv1.Ingress {
	return func(g gomega.Gomega) *networkingv1.Ingress {
		ingress, err := t.Client().Core().Cluster(logicalcluster.From(namespace)).NetworkingV1().Ingresses(namespace.Name).Get(t.Ctx(), name, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return ingress
	}
}

func GetIngresses(t Test, namespace *corev1.Namespace, labelSelector string) []networkingv1.Ingress {
	t.T().Helper()
	return Ingresses(t, namespace, labelSelector)(t)
}

func Ingresses(t Test, namespace *corev1.Namespace, labelSelector string) func(g gomega.Gomega) []networkingv1.Ingress {
	return func(g gomega.Gomega) []networkingv1.Ingress {
		ingresses, err := t.Client().Core().Cluster(logicalcluster.From(namespace)).NetworkingV1().Ingresses(namespace.Name).List(t.Ctx(), metav1.ListOptions{LabelSelector: labelSelector})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return ingresses.Items
	}
}

func LoadBalancerIngresses(ingress *networkingv1.Ingress) []corev1.LoadBalancerIngress {
	return ingress.Status.LoadBalancer.Ingress
}

func IngressTLS(ingress *networkingv1.Ingress) []networkingv1.IngressTLS {
	return ingress.Spec.TLS
}

// HostsEqualsToGeneratedHost checks Ingress hosts are the same as the generated hosts
func HostsEqualsToGeneratedHost(ingress *networkingv1.Ingress) bool {
	equals := true
	for _, rule := range ingress.Spec.Rules {
		if rule.Host != Annotations(ingress)[kuadrantcluster.ANNOTATION_HCG_HOST] {
			equals = false
		}
	}
	return equals
}
