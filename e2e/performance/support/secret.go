//go:build performance

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
)

func GetSecret(t Test, namespace *corev1.Namespace, name string) *corev1.Secret {
	t.T().Helper()
	return Secret(t, namespace, name)(t)
}

func Secret(t Test, namespace *corev1.Namespace, name string) func(g gomega.Gomega) *corev1.Secret {
	return func(g gomega.Gomega) *corev1.Secret {
		secret, err := t.Client().Core().CoreV1().Secrets(namespace.Name).Get(t.Ctx(), name, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return secret
	}
}

func GetSecrets(t Test, namespace *corev1.Namespace, labelSelector string) []corev1.Secret {
	t.T().Helper()
	return Secrets(t, namespace, labelSelector)(t)
}

func Secrets(t Test, namespace *corev1.Namespace, labelSelector string) func(g gomega.Gomega) []corev1.Secret {
	return func(g gomega.Gomega) []corev1.Secret {
		secrets, err := t.Client().Core().CoreV1().Secrets(namespace.Name).List(t.Ctx(), metav1.ListOptions{LabelSelector: labelSelector})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return secrets.Items
	}
}
