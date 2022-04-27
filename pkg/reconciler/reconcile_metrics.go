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

package reconciler

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/kuadrant/kcp-glbc/pkg/metrics"
)

const (
	controllerLabel = "controller"
	resultLabel     = "result"

	labelError   = "error"
	labelSuccess = "success"
)

var (
	// reconcileTotal is a prometheus counter metrics which holds the total
	// number of reconciliations per controller. It has two labels. controller label refers
	// to the controller name and result label refers to the reconciliation result i.e.
	// success, error, requeue, requeue_after.
	reconcileTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "glbc_controller_reconcile_total",
		Help: "Total number of reconciliations per controller",
	}, []string{controllerLabel, resultLabel})

	// reconcileErrors is a prometheus counter metrics which holds the total
	// number of errors from the Reconciler.
	reconcileErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "glbc_controller_reconcile_errors_total",
		Help: "Total number of reconciliation errors per controller",
	}, []string{controllerLabel})

	// reconcileTime is a prometheus metric which keeps track of the duration
	// of reconciliations.
	reconcileTime = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "glbc_controller_reconcile_time_seconds",
		Help: "Length of time per reconciliation per controller",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.35, 0.4, 0.45, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0,
			1.25, 1.5, 1.75, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5, 6, 7, 8, 9, 10, 15, 20, 25, 30, 40, 50, 60},
	}, []string{controllerLabel})

	// workerCount is a prometheus metric which holds the number of
	// concurrent reconciles per controller.
	workerCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "glbc_controller_max_concurrent_reconciles",
		Help: "Maximum number of concurrent reconciles per controller",
	}, []string{controllerLabel})

	// activeWorkers is a prometheus metric which holds the number
	// of active workers per controller.
	activeWorkers = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "glbc_controller_active_workers",
		Help: "Number of currently used workers per controller",
	}, []string{controllerLabel})
)

func init() {
	metrics.Registry.MustRegister(
		reconcileTotal,
		reconcileErrors,
		reconcileTime,
		workerCount,
		activeWorkers,
	)
}

func initMetrics(c *Controller) {
	activeWorkers.WithLabelValues(c.Name).Set(0)
	reconcileErrors.WithLabelValues(c.Name).Add(0)
	reconcileTotal.WithLabelValues(c.Name, labelError).Add(0)
	reconcileTotal.WithLabelValues(c.Name, labelSuccess).Add(0)
	workerCount.WithLabelValues(c.Name).Set(0.0)
}
