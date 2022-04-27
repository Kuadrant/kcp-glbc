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

package tls

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/kuadrant/kcp-glbc/pkg/metrics"
	"github.com/kuadrant/kcp-glbc/pkg/tls"
)

const (
	issuerLabel          = "issuer"
	hostnameLabel        = "hostname"
	resultLabel          = "result"
	resultLabelSucceeded = "succeeded"
	// FIXME: Refactor TLS certificate management to be able to monitor errors
	//nolint:unused
	resultLabelFailed = "failed"
)

var (
	// tlsCertificateRequestTotal is a prometheus counter metrics which holds the total
	// number of TLS certificate requests.
	tlsCertificateRequestTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "glbc_tls_certificate_request_total",
			Help: "GLBC TLS certificate total number of requests",
		},
		[]string{
			issuerLabel,
			hostnameLabel,
			resultLabel,
		},
	)

	// tlsCertificateRequestErrors is a prometheus counter metrics which holds the total
	// number of failed TLS certificate requests.
	tlsCertificateRequestErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "glbc_tls_certificate_request_errors_total",
			Help: "GLBC TLS certificate total number of request errors",
		},
		// TODO: check if it's possible to add an error/code label
		[]string{
			issuerLabel,
			hostnameLabel,
		},
	)

	// tlsCertificateIssuanceDuration is a prometheus metric which records the duration
	// of TLS certificate issuance.
	tlsCertificateIssuanceDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "glbc_tls_certificate_issuance_duration_seconds",
			Help: "GLBC TLS certificate issuance duration",
			Buckets: []float64{
				1 * time.Second.Seconds(),
				5 * time.Second.Seconds(),
				10 * time.Second.Seconds(),
				15 * time.Second.Seconds(),
				30 * time.Second.Seconds(),
				45 * time.Second.Seconds(),
				1 * time.Minute.Seconds(),
				2 * time.Minute.Seconds(),
				5 * time.Minute.Seconds(),
			},
		},
		[]string{
			issuerLabel,
			hostnameLabel,
			resultLabel,
		},
	)

	// tlsCertificateSecretCount is a prometheus metric which holds the number of
	// TLS certificates currently managed.
	tlsCertificateSecretCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "glbc_tls_certificate_secret_count",
			Help: "GLBC TLS certificate secret count",
		},
		[]string{
			issuerLabel,
			hostnameLabel,
		},
	)
)

func init() {
	// Register metrics with the global prometheus registry
	metrics.Registry.MustRegister(
		tlsCertificateRequestTotal,
		tlsCertificateRequestErrors,
		tlsCertificateIssuanceDuration,
		tlsCertificateSecretCount,
	)
}

func InitMetrics(provider tls.Provider) {
	// Initialize metrics
	issuer := provider.IssuerID()
	for _, domain := range provider.Domains() {
		tlsCertificateRequestTotal.WithLabelValues(issuer, domain, resultLabelSucceeded).Add(0)
		tlsCertificateRequestTotal.WithLabelValues(issuer, domain, resultLabelFailed).Add(0)
		tlsCertificateRequestErrors.WithLabelValues(issuer, domain).Add(0)
		tlsCertificateSecretCount.WithLabelValues(issuer, domain).Set(0)
	}

	tls.InitMetrics(provider)
}
