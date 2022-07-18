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
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kcp-dev/logicalcluster"
)

func GetServices(t Test, namespace *corev1.Namespace, labelSelector string) []corev1.Service {
	t.T().Helper()
	return Services(t, namespace, labelSelector)(t)
}

func Services(t Test, namespace *corev1.Namespace, labelSelector string) func(g gomega.Gomega) []corev1.Service {
	return func(g gomega.Gomega) []corev1.Service {
		services, err := t.Client().Core().Cluster(logicalcluster.From(namespace)).CoreV1().Services(namespace.Name).List(t.Ctx(), metav1.ListOptions{LabelSelector: labelSelector})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return services.Items
	}
}
