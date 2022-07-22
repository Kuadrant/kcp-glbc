//go:build performance

package performance

import (
	"fmt"
	"os"
	"testing"

	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"github.com/rs/xid"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/kuadrant/kcp-glbc/e2e/performance/support"
	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	kuadrantcluster "github.com/kuadrant/kcp-glbc/pkg/cluster"
	"github.com/kuadrant/kcp-glbc/pkg/util/env"
)

func createTestIngress(t Test, namespace *corev1.Namespace) *networkingv1.Ingress {
	name := "perf-test-" + xid.New().String()

	ingress, err := t.Client().Core().NetworkingV1().Ingresses(namespace.Name).
		Apply(t.Ctx(), IngressConfiguration(namespace.Name, name), ApplyOptions)
	t.Expect(err).NotTo(HaveOccurred())

	return ingress
}

func deleteTestIngress(t Test, ingress *networkingv1.Ingress) {
	propagationPolicy := metav1.DeletePropagationBackground
	err := t.Client().Core().NetworkingV1().Ingresses(ingress.Namespace).Delete(t.Ctx(), ingress.Name, metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
	t.Expect(err).NotTo(HaveOccurred())
}

func TestIngress(t *testing.T) {
	test := With(t)
	test.T().Parallel()

	// To run this test you must know the DNSZone and Domain the target GLBC instance is configured to use.
	// Check the a DNS zone id has been set
	zoneID := os.Getenv("AWS_DNS_PUBLIC_ZONE_ID")
	test.Expect(zoneID).NotTo(Equal(""))
	// Check the a DNS domain name has been set
	glbcDomain := os.Getenv("GLBC_DOMAIN")
	test.Expect(glbcDomain).NotTo(Equal(""))

	// Create a namespace
	namespace := test.NewTestNamespace()
	test.Expect(namespace).NotTo(BeNil())

	ingressCount := env.GetEnvInt(TestIngressCount, DefaultTestIngressCount)
	test.Expect(ingressCount > 0).To(BeTrue())
	test.T().Log(fmt.Sprintf("Creating %d Ingresses", ingressCount))

	name := "perf-test-echo"
	// Create test Deployment
	_, err := test.Client().Core().AppsV1().Deployments(namespace.Name).
		Apply(test.Ctx(), DeploymentConfiguration(namespace.Name, name), ApplyOptions)
	test.Expect(err).NotTo(HaveOccurred())

	// Create test Service
	_, err = test.Client().Core().CoreV1().Services(namespace.Name).
		Apply(test.Ctx(), ServiceConfiguration(namespace.Name, name, map[string]string{}), ApplyOptions)
	test.Expect(err).NotTo(HaveOccurred())

	// Create Ingresses
	for i := 1; i <= ingressCount; i++ {
		ingress := createTestIngress(test, namespace)
		test.Expect(ingress).NotTo(BeNil())
	}

	// Retrieve Ingresses
	ingresses := GetIngresses(test, namespace, "")
	test.Expect(ingresses).Should(HaveLen(ingressCount))

	// Assert Ingresses reconcile success
	for _, ingress := range ingresses {
		test.Eventually(Ingress(test, namespace, ingress.Name)).WithTimeout(TestTimeoutMedium).Should(And(
			WithTransform(Annotations, And(
				HaveKey(kuadrantcluster.ANNOTATION_HCG_HOST),
				HaveKey(kuadrantcluster.ANNOTATION_HCG_CUSTOM_HOST_REPLACED)),
			),
			WithTransform(LoadBalancerIngresses, HaveLen(1)),
		))
	}

	// Retrieve DNSRecords
	test.Eventually(DNSRecords(test, namespace, "")).Should(HaveLen(ingressCount))
	dnsRecords := GetDNSRecords(test, namespace, "")
	test.Expect(dnsRecords).Should(HaveLen(ingressCount))

	// Assert DNSRecords reconcile success
	for _, record := range dnsRecords {
		test.Eventually(DNSRecord(test, namespace, record.Name)).Should(And(
			WithTransform(DNSRecordEndpoints, HaveLen(1)),
			WithTransform(DNSRecordCondition(zoneID, kuadrantv1.DNSRecordFailedConditionType), MatchFieldsP(IgnoreExtras,
				Fields{
					"Status":  Equal("False"),
					"Reason":  Equal("ProviderSuccess"),
					"Message": Equal("The DNS provider succeeded in ensuring the record"),
				})),
		))
	}

	// Retrieve TLS Secrets
	test.Eventually(Secrets(test, namespace, "kuadrant.dev/hcg.managed=true")).WithTimeout(
		TestTimeoutLong).Should(HaveLen(ingressCount))

	// Delete Ingresses
	for _, ingress := range ingresses {
		deleteTestIngress(test, &ingress)
	}

	// Assert Ingresses, DNSRecord and TLS Secret deletion success
	test.Eventually(Ingresses(test, namespace, "")).Should(HaveLen(0))
	test.Eventually(DNSRecords(test, namespace, "")).Should(HaveLen(0))
	// ToDo Uncomment this as part of the KCP 0.6 upgrade.
	// Currently a finalizer is left on teh secret that never gets removed preventing it from ever deleting
	//test.Eventually(Secrets(test, namespace, "kuadrant.dev/hcg.managed=true")).Should(HaveLen(0))

	// Finally, delete the test deployment and service resources
	test.Expect(test.Client().Core().CoreV1().Services(namespace.Name).
		Delete(test.Ctx(), name, metav1.DeleteOptions{})).
		To(Succeed())
	test.Expect(test.Client().Core().AppsV1().Deployments(namespace.Name).
		Delete(test.Ctx(), name, metav1.DeleteOptions{})).
		To(Succeed())

}
