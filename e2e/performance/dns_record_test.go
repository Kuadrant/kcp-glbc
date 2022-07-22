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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/kuadrant/kcp-glbc/e2e/performance/support"
	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	"github.com/kuadrant/kcp-glbc/pkg/util/env"
)

func createTestDNSRecord(t Test, namespace *corev1.Namespace, domain string) *kuadrantv1.DNSRecord {
	name := "perf-test-" + xid.New().String()

	dnsRecord := &kuadrantv1.DNSRecord{
		TypeMeta: metav1.TypeMeta{
			APIVersion: kuadrantv1.SchemeGroupVersion.String(),
			Kind:       "DNSRecord",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace.Name,
		},
		Spec: kuadrantv1.DNSRecordSpec{
			Endpoints: []*kuadrantv1.Endpoint{
				{
					DNSName:    fmt.Sprintf("%s.%s", name, domain),
					Targets:    []string{"127.0.0.1"},
					RecordType: "A",
					RecordTTL:  60,
				},
			},
		},
	}

	dnsRecord, err := t.Client().Kuadrant().KuadrantV1().DNSRecords(namespace.Name).Create(t.Ctx(), dnsRecord, metav1.CreateOptions{})
	t.Expect(err).NotTo(HaveOccurred())

	return dnsRecord
}

func deleteTestDNSRecord(t Test, dnsRecord *kuadrantv1.DNSRecord) {
	propagationPolicy := metav1.DeletePropagationBackground
	err := t.Client().Kuadrant().KuadrantV1().DNSRecords(dnsRecord.Namespace).Delete(t.Ctx(), dnsRecord.Name, metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
	t.Expect(err).NotTo(HaveOccurred())
}

func TestDNSRecord(t *testing.T) {
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

	dnsRecordCount := env.GetEnvInt(TestDNSRecordCount, DefaultTestDNSRecordCount)
	test.Expect(dnsRecordCount > 0).To(BeTrue())
	test.T().Log(fmt.Sprintf("Creating %d DNSRecords", dnsRecordCount))

	// Create DNSRecords
	for i := 1; i <= dnsRecordCount; i++ {
		dnsRecord := createTestDNSRecord(test, namespace, glbcDomain)
		test.Expect(dnsRecord).NotTo(BeNil())
	}

	// Retrieve DNSRecords
	dnsRecords := GetDNSRecords(test, namespace, "")
	test.Expect(dnsRecords).Should(HaveLen(dnsRecordCount))

	// Assert provider success
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

	// Delete DNSRecords
	for _, record := range dnsRecords {
		deleteTestDNSRecord(test, &record)
	}

	test.Eventually(DNSRecords(test, namespace, "")).Should(HaveLen(0))

}
