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

package metrics

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	prometheus "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kcp-dev/apimachinery/pkg/logicalcluster"
	apisv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/apis/v1alpha1"
	conditionsapi "github.com/kcp-dev/kcp/third_party/conditions/apis/conditions/v1alpha1"

	. "github.com/kuadrant/kcp-glbc/e2e/support"
	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	kuadrantcluster "github.com/kuadrant/kcp-glbc/pkg/cluster"
)

const issuer = "glbc-ca"

func TestMetrics(t *testing.T) {
	test := With(t)

	// Create the test workspace
	workspace := test.NewTestWorkspace()

	// Import the GLBC APIs
	binding := test.NewGLBCAPIBinding(InWorkspace(workspace))

	// Wait until the APIBinding is actually in bound phase
	test.Eventually(APIBinding(test, binding.ClusterName, binding.Name)).
		Should(WithTransform(APIBindingPhase, Equal(apisv1alpha1.APIBindingPhaseBound)))

	// And check the APIs are imported into the workspace
	test.Expect(HasImportedAPIs(test, workspace, kuadrantv1.SchemeGroupVersion.WithKind("DNSRecord"))(test)).
		Should(BeTrue())

	// Register workload cluster 1 into the test workspace
	cluster1 := test.NewWorkloadCluster("kcp-cluster-1", InWorkspace(workspace), WithKubeConfigByName, Syncer().ResourcesToSync(GLBCResources...))

	// Wait until cluster 1 is ready
	test.Eventually(WorkloadCluster(test, cluster1.ClusterName, cluster1.Name)).WithTimeout(time.Minute * 3).Should(WithTransform(
		ConditionStatus(conditionsapi.ReadyCondition),
		Equal(corev1.ConditionTrue),
	))

	// Wait until the APIs are imported into the workspace
	test.Eventually(HasImportedAPIs(test, workspace,
		corev1.SchemeGroupVersion.WithKind("Service"),
		appsv1.SchemeGroupVersion.WithKind("Deployment"),
		networkingv1.SchemeGroupVersion.WithKind("Ingress"),
	)).Should(BeTrue())

	// Create a namespace
	namespace := test.NewTestNamespace(InWorkspace(workspace))

	name := "echo"

	// Create the Deployment
	_, err := test.Client().Core().Cluster(logicalcluster.From(namespace)).AppsV1().Deployments(namespace.Name).
		Apply(test.Ctx(), DeploymentConfiguration(namespace.Name, name), ApplyOptions)
	test.Expect(err).NotTo(HaveOccurred())

	// Create the Service
	_, err = test.Client().Core().Cluster(logicalcluster.From(namespace)).CoreV1().Services(namespace.Name).
		Apply(test.Ctx(), ServiceConfiguration(namespace.Name, name, map[string]string{}), ApplyOptions)
	test.Expect(err).NotTo(HaveOccurred())

	// Create the Ingress, it's delayed and run in a separate Go routine, to mitigate the race
	// where cert-manager is being too prompt to issue the TLS certificate (which turns out to be quick fast
	// when using a CA issuer), and the below assertion happens too late to detect the pending TLS certificate request.
	timer := time.AfterFunc(2*time.Second, func() {
		_, err = test.Client().Core().Cluster(logicalcluster.From(namespace)).NetworkingV1().Ingresses(namespace.Name).
			Apply(test.Ctx(), IngressConfiguration(namespace.Name, name), ApplyOptions)
		test.Expect(err).NotTo(HaveOccurred())
	})
	t.Cleanup(func() {
		timer.Stop()
	})

	hostname := ""

	// We pull the metrics aggressively as the certificate can be issued quickly when using the CA issuer.
	// We may want to adjust the pull interval as well as the timeout based on the configured issuer.
	test.Eventually(Metrics(test.Ctx()), TestTimeoutMedium, 10*time.Millisecond).Should(And(
		HaveKey("glbc_tls_certificate_pending_request_count"),
		WithTransform(Metric("glbc_tls_certificate_pending_request_count"), Satisfy(
			func(m *prometheus.MetricFamily) bool {
				ingress, err := test.Client().Core().Cluster(logicalcluster.From(namespace)).NetworkingV1().Ingresses(namespace.Name).Get(test.Ctx(), name, metav1.GetOptions{})
				if err != nil {
					if apierrors.IsNotFound(err) {
						return false
					}
					t.Fatal(err)
				}
				hostname = ingress.Annotations[kuadrantcluster.ANNOTATION_HCG_HOST]

				match, _ := EqualP(prometheus.MetricFamily{
					Name: stringP("glbc_tls_certificate_pending_request_count"),
					Help: stringP("GLBC TLS certificate pending request count"),
					Type: metricTypeP(prometheus.MetricType_GAUGE),
					Metric: []*prometheus.Metric{
						{
							Label: []*prometheus.LabelPair{
								label("issuer", issuer),
							},
							Gauge: &prometheus.Gauge{
								Value: float64P(1),
							},
						},
					},
				}).Match(m)

				return match
			},
		)),
	))

	secretName := strings.ReplaceAll(fmt.Sprintf("%s-%s-%s", namespace.GetClusterName(), namespace.Name, name), ":", "")

	// Wait until the Ingress is reconciled with the load balancer Ingresses
	test.Eventually(Ingress(test, namespace, name)).WithTimeout(TestTimeoutMedium).Should(And(
		// Host spec
		WithTransform(Annotations, And(
			HaveKey(kuadrantcluster.ANNOTATION_HCG_HOST),
			HaveKey(kuadrantcluster.ANNOTATION_HCG_CUSTOM_HOST_REPLACED)),
		),
		Satisfy(HostsEqualsToGeneratedHost),
		// TLS certificate spec
		WithTransform(IngressTLS, ConsistOf(networkingv1.IngressTLS{
			Hosts:      []string{hostname},
			SecretName: secretName,
		})),
		// Load balancer status
		WithTransform(LoadBalancerIngresses, HaveLen(1)),
	))

	// Check the TLS Secret
	test.Eventually(Secret(test, namespace, secretName)).
		WithTimeout(TestTimeoutMedium).
		Should(WithTransform(Certificate, PointTo(MatchFields(IgnoreExtras,
			map[string]types.GomegaMatcher{
				"DNSNames": ConsistOf(hostname),
			},
		))))

	// Check the metrics
	metrics, err := getMetrics(test.Ctx())
	test.Expect(err).NotTo(HaveOccurred())

	test.Expect(metrics).To(And(
		HaveKey("glbc_tls_certificate_pending_request_count"),
		WithTransform(Metric("glbc_tls_certificate_pending_request_count"), EqualP(
			prometheus.MetricFamily{
				Name: stringP("glbc_tls_certificate_pending_request_count"),
				Help: stringP("GLBC TLS certificate pending request count"),
				Type: metricTypeP(prometheus.MetricType_GAUGE),
				Metric: []*prometheus.Metric{
					{
						Label: []*prometheus.LabelPair{
							label("issuer", issuer),
						},
						Gauge: &prometheus.Gauge{
							Value: float64P(0),
						},
					},
				},
			},
		)),
	))

	test.Expect(metrics).To(And(
		HaveKey("glbc_tls_certificate_request_total"),
		WithTransform(Metric("glbc_tls_certificate_request_total"), EqualP(
			prometheus.MetricFamily{
				Name: stringP("glbc_tls_certificate_request_total"),
				Help: stringP("GLBC TLS certificate total number of requests"),
				Type: metricTypeP(prometheus.MetricType_COUNTER),
				Metric: []*prometheus.Metric{
					{
						Label: []*prometheus.LabelPair{
							label("issuer", issuer),
							label("result", "failed"),
						},
						Counter: &prometheus.Counter{
							Value: float64P(0),
						},
					},
					{
						Label: []*prometheus.LabelPair{
							label("issuer", issuer),
							label("result", "succeeded"),
						},
						Counter: &prometheus.Counter{
							Value: float64P(1),
						},
					},
				},
			},
		)),
	))

	test.Expect(metrics).To(And(
		HaveKey("glbc_tls_certificate_request_errors_total"),
		WithTransform(Metric("glbc_tls_certificate_request_errors_total"), EqualP(
			prometheus.MetricFamily{
				Name: stringP("glbc_tls_certificate_request_errors_total"),
				Help: stringP("GLBC TLS certificate total number of request errors"),
				Type: metricTypeP(prometheus.MetricType_COUNTER),
				Metric: []*prometheus.Metric{
					{
						Label: []*prometheus.LabelPair{
							label("issuer", issuer),
						},
						Counter: &prometheus.Counter{
							Value: float64P(0),
						},
					},
				},
			}),
		),
	))

	test.Expect(metrics).To(And(
		HaveKey("glbc_tls_certificate_secret_count"),
		WithTransform(Metric("glbc_tls_certificate_secret_count"), MatchFieldsP(IgnoreExtras,
			Fields{
				"Name": EqualP("glbc_tls_certificate_secret_count"),
				"Help": EqualP("GLBC TLS certificate secret count"),
				"Type": EqualP(prometheus.MetricType_GAUGE),
				"Metric": ContainElement(&prometheus.Metric{
					Label: []*prometheus.LabelPair{
						label("issuer", issuer),
					},
					Gauge: &prometheus.Gauge{
						Value: float64P(1),
					},
				}),
			},
		)),
	))

	ingress := GetIngress(test, namespace, name)
	secret := GetSecret(test, namespace, ingress.Spec.TLS[0].SecretName)
	// Ingress creation timestamp is serialized to RFC3339 format and set in an annotation on the certificate request
	duration := secret.CreationTimestamp.Sub(ingress.CreationTimestamp.Rfc3339Copy().Time).Seconds()
	test.Expect(metrics).To(And(
		HaveKey("glbc_tls_certificate_issuance_duration_seconds"),
		WithTransform(Metric("glbc_tls_certificate_issuance_duration_seconds"), EqualP(
			prometheus.MetricFamily{
				Name: stringP("glbc_tls_certificate_issuance_duration_seconds"),
				Help: stringP("GLBC TLS certificate issuance duration"),
				Type: metricTypeP(prometheus.MetricType_HISTOGRAM),
				Metric: []*prometheus.Metric{
					{
						Label: []*prometheus.LabelPair{
							label("issuer", issuer),
							label("result", "succeeded"),
						},
						Histogram: &prometheus.Histogram{
							SampleCount: uint64P(1),
							SampleSum:   float64P(duration),
							Bucket: buckets(duration, []float64{
								1 * time.Second.Seconds(),
								5 * time.Second.Seconds(),
								10 * time.Second.Seconds(),
								15 * time.Second.Seconds(),
								30 * time.Second.Seconds(),
								45 * time.Second.Seconds(),
								1 * time.Minute.Seconds(),
								2 * time.Minute.Seconds(),
								5 * time.Minute.Seconds(),
								math.Inf(1),
							}),
						},
					},
				},
			},
		)),
	))

	// Finally, delete the Ingress and assert the metrics to cover the entire lifecycle
	test.Expect(test.Client().Core().Cluster(logicalcluster.From(namespace)).NetworkingV1().Ingresses(namespace.Name).
		Delete(test.Ctx(), name, metav1.DeleteOptions{})).
		To(Succeed())

	// Only the TLS certificate Secret count should change
	test.Eventually(Metrics(test.Ctx()), TestTimeoutShort).Should(And(
		HaveKey("glbc_tls_certificate_secret_count"),
		WithTransform(Metric("glbc_tls_certificate_secret_count"), MatchFieldsP(IgnoreExtras,
			Fields{
				"Name": EqualP("glbc_tls_certificate_secret_count"),
				"Help": EqualP("GLBC TLS certificate secret count"),
				"Type": EqualP(prometheus.MetricType_GAUGE),
				"Metric": ContainElement(&prometheus.Metric{
					Label: []*prometheus.LabelPair{
						label("issuer", issuer),
					},
					Gauge: &prometheus.Gauge{
						Value: float64P(0),
					},
				}),
			},
		)),
	))

	// The other metrics should not be updated
	test.Consistently(Metrics(test.Ctx()), 15*time.Second).Should(And(
		HaveKey("glbc_tls_certificate_pending_request_count"),
		WithTransform(Metric("glbc_tls_certificate_pending_request_count"), EqualP(
			prometheus.MetricFamily{
				Name: stringP("glbc_tls_certificate_pending_request_count"),
				Help: stringP("GLBC TLS certificate pending request count"),
				Type: metricTypeP(prometheus.MetricType_GAUGE),
				Metric: []*prometheus.Metric{
					{
						Label: []*prometheus.LabelPair{
							label("issuer", issuer),
						},
						Gauge: &prometheus.Gauge{
							Value: float64P(0),
						},
					},
				},
			},
		)),
		HaveKey("glbc_tls_certificate_request_total"),
		WithTransform(Metric("glbc_tls_certificate_request_total"), EqualP(
			prometheus.MetricFamily{
				Name: stringP("glbc_tls_certificate_request_total"),
				Help: stringP("GLBC TLS certificate total number of requests"),
				Type: metricTypeP(prometheus.MetricType_COUNTER),
				Metric: []*prometheus.Metric{
					{
						Label: []*prometheus.LabelPair{
							label("issuer", issuer),
							label("result", "failed"),
						},
						Counter: &prometheus.Counter{
							Value: float64P(0),
						},
					},
					{
						Label: []*prometheus.LabelPair{
							label("issuer", issuer),
							label("result", "succeeded"),
						},
						Counter: &prometheus.Counter{
							Value: float64P(1),
						},
					},
				},
			},
		)),
		HaveKey("glbc_tls_certificate_request_errors_total"),
		WithTransform(Metric("glbc_tls_certificate_request_errors_total"), EqualP(
			prometheus.MetricFamily{
				Name: stringP("glbc_tls_certificate_request_errors_total"),
				Help: stringP("GLBC TLS certificate total number of request errors"),
				Type: metricTypeP(prometheus.MetricType_COUNTER),
				Metric: []*prometheus.Metric{
					{
						Label: []*prometheus.LabelPair{
							label("issuer", issuer),
						},
						Counter: &prometheus.Counter{
							Value: float64P(0),
						},
					},
				},
			}),
		),
		HaveKey("glbc_tls_certificate_issuance_duration_seconds"),
		WithTransform(Metric("glbc_tls_certificate_issuance_duration_seconds"), EqualP(
			prometheus.MetricFamily{
				Name: stringP("glbc_tls_certificate_issuance_duration_seconds"),
				Help: stringP("GLBC TLS certificate issuance duration"),
				Type: metricTypeP(prometheus.MetricType_HISTOGRAM),
				Metric: []*prometheus.Metric{
					{
						Label: []*prometheus.LabelPair{
							label("issuer", issuer),
							label("result", "succeeded"),
						},
						Histogram: &prometheus.Histogram{
							SampleCount: uint64P(1),
							SampleSum:   float64P(duration),
							Bucket: buckets(duration, []float64{
								1 * time.Second.Seconds(),
								5 * time.Second.Seconds(),
								10 * time.Second.Seconds(),
								15 * time.Second.Seconds(),
								30 * time.Second.Seconds(),
								45 * time.Second.Seconds(),
								1 * time.Minute.Seconds(),
								2 * time.Minute.Seconds(),
								5 * time.Minute.Seconds(),
								math.Inf(1),
							}),
						},
					},
				},
			},
		)),
	))
}

func Metric(metric string) func(metrics map[string]*prometheus.MetricFamily) *prometheus.MetricFamily {
	return func(metrics map[string]*prometheus.MetricFamily) *prometheus.MetricFamily {
		return metrics[metric]
	}
}

func Metrics(ctx context.Context) func(g Gomega) map[string]*prometheus.MetricFamily {
	return func(g Gomega) map[string]*prometheus.MetricFamily {
		m, err := getMetrics(ctx)
		g.Expect(err).NotTo(HaveOccurred())
		return m
	}
}

func getMetrics(ctx context.Context) (map[string]*prometheus.MetricFamily, error) {
	data, err := requestMetrics(ctx)
	if err != nil {
		return nil, err
	}
	return parsePrometheusData(data)
}

func requestMetrics(ctx context.Context) (data []byte, err error) {
	// This should be made configurable, or eventually retrieved from Pod logs if run in-cluster
	request, err := http.NewRequestWithContext(ctx, "GET", "http://localhost:8080/metrics", nil)
	if err != nil {
		return
	}
	client := http.Client{}
	response, err := client.Do(request)
	if err != nil {
		return
	}

	defer func(Body io.ReadCloser) {
		e := Body.Close()
		if err == nil {
			err = e
		}
	}(response.Body)

	data, err = io.ReadAll(response.Body)
	return
}

// https://prometheus.io/docs/instrumenting/exposition_formats/
func parsePrometheusData(data []byte) (map[string]*prometheus.MetricFamily, error) {
	var parser expfmt.TextParser
	return parser.TextToMetricFamilies(bytes.NewReader(data))
}

func label(name, value string) *prometheus.LabelPair {
	return &prometheus.LabelPair{
		Name:  &name,
		Value: &value,
	}
}

func bucket(value float64, upperBound float64) *prometheus.Bucket {
	var count uint64
	if value <= upperBound {
		count++
	}
	return &prometheus.Bucket{
		UpperBound:      float64P(upperBound),
		CumulativeCount: &count,
	}
}

func buckets(value float64, upperBounds []float64) []*prometheus.Bucket {
	var buckets []*prometheus.Bucket
	for _, upperBound := range upperBounds {
		buckets = append(buckets, bucket(value, upperBound))
	}
	return buckets
}

func stringP(s string) *string {
	return &s
}

func metricTypeP(t prometheus.MetricType) *prometheus.MetricType {
	return &t
}

func uint64P(i uint64) *uint64 {
	return &i
}

func float64P(f float64) *float64 {
	return &f
}
