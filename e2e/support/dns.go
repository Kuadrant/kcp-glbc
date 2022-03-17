//go:build e2e
// +build e2e

package support

import (
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
)

func GetDNSRecord(t Test, namespace *corev1.Namespace, name string) *kuadrantv1.DNSRecord {
	return DNSRecord(t, namespace, name)(t)
}

func DNSRecord(t Test, namespace *corev1.Namespace, name string) func(g gomega.Gomega) *kuadrantv1.DNSRecord {
	return func(g gomega.Gomega) *kuadrantv1.DNSRecord {
		dnsRecord, err := t.Client().Kuadrant().Cluster(namespace.ClusterName).KuadrantV1().DNSRecords(namespace.Name).Get(t.Ctx(), name, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return dnsRecord
	}
}
