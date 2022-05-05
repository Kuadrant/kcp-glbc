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
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	appsv1apply "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	v1apply "k8s.io/client-go/applyconfigurations/meta/v1"
	networkingv1apply "k8s.io/client-go/applyconfigurations/networking/v1"
)

func IngressConfiguration(namespace, name string) *networkingv1apply.IngressApplyConfiguration {
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

func DeploymentConfiguration(namespace, name string) *appsv1apply.DeploymentApplyConfiguration {
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

func ServiceConfiguration(namespace, name string, annotations map[string]string) *corev1apply.ServiceApplyConfiguration {
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
