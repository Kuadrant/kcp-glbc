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
	"io"
	"net/http"

	"github.com/onsi/gomega"

	prometheus "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"

	. "github.com/kuadrant/kcp-glbc/e2e/support"
)

func Metric(metric string) func(metrics map[string]*prometheus.MetricFamily) *prometheus.MetricFamily {
	return func(metrics map[string]*prometheus.MetricFamily) *prometheus.MetricFamily {
		return metrics[metric]
	}
}

func GetMetric(t Test, metric string) *prometheus.MetricFamily {
	t.T().Helper()
	metrics := GetMetrics(t)
	t.Expect(metrics).To(gomega.HaveKey(metric))
	return metrics[metric]
}

func Metrics(t Test) func() map[string]*prometheus.MetricFamily {
	return func() map[string]*prometheus.MetricFamily {
		return GetMetrics(t)
	}
}

func GetMetrics(t Test) map[string]*prometheus.MetricFamily {
	t.T().Helper()
	data, err := requestMetrics(t.Ctx())
	t.Expect(err).NotTo(gomega.HaveOccurred())
	metrics, err := parsePrometheusData(data)
	t.Expect(err).NotTo(gomega.HaveOccurred())
	return metrics
}

func requestMetrics(ctx context.Context) (data []byte, err error) {
	// TODO: this should be made configurable
	request, err := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:8080/metrics", nil)
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
	if value > 0 && value <= upperBound {
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
