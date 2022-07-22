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

	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
)

func GetDNSRecord(t Test, namespace *corev1.Namespace, name string) *kuadrantv1.DNSRecord {
	t.T().Helper()
	return DNSRecord(t, namespace, name)(t)
}

func DNSRecord(t Test, namespace *corev1.Namespace, name string) func(g gomega.Gomega) *kuadrantv1.DNSRecord {
	return func(g gomega.Gomega) *kuadrantv1.DNSRecord {
		dnsRecord, err := t.Client().Kuadrant().KuadrantV1().DNSRecords(namespace.Name).Get(t.Ctx(), name, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return dnsRecord
	}
}

func GetDNSRecords(t Test, namespace *corev1.Namespace, labelSelector string) []kuadrantv1.DNSRecord {
	t.T().Helper()
	return DNSRecords(t, namespace, labelSelector)(t)
}

func DNSRecords(t Test, namespace *corev1.Namespace, labelSelector string) func(g gomega.Gomega) []kuadrantv1.DNSRecord {
	return func(g gomega.Gomega) []kuadrantv1.DNSRecord {
		dnsRecords, err := t.Client().Kuadrant().KuadrantV1().DNSRecords(namespace.Name).List(t.Ctx(), metav1.ListOptions{LabelSelector: labelSelector})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return dnsRecords.Items
	}
}

func DNSRecordEndpoints(record *kuadrantv1.DNSRecord) []*kuadrantv1.Endpoint {
	return record.Spec.Endpoints
}

func DNSRecordCondition(zoneID, condition string) func(record *kuadrantv1.DNSRecord) *kuadrantv1.DNSZoneCondition {
	return func(record *kuadrantv1.DNSRecord) *kuadrantv1.DNSZoneCondition {
		for _, z := range record.Status.Zones {
			if z.DNSZone.ID != zoneID {
				continue
			}
			for _, c := range z.Conditions {
				if c.Type == condition {
					return &c
				}
			}
			return nil
		}
		return nil
	}
}
