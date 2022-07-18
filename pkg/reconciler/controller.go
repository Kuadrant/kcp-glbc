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
	"context"
	"time"

	"github.com/go-logr/logr"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"github.com/kuadrant/kcp-glbc/pkg/log"
)

type Controller struct {
	Name    string
	Queue   workqueue.RateLimitingInterface
	Process func(context.Context, string) error
	Logger  logr.Logger
}

func NewController(name string, queue workqueue.RateLimitingInterface) *Controller {
	controller := &Controller{
		Name:   name,
		Queue:  queue,
		Logger: log.Logger.WithName(name),
	}
	initMetrics(controller)
	return controller
}

func (c *Controller) Enqueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}
	c.Queue.Add(key)
}

func (c *Controller) EnqueueAfter(obj interface{}, dur time.Duration) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}
	c.Queue.AddAfter(key, dur)
}

func (c *Controller) Start(ctx context.Context, numThreads int) {
	defer runtime.HandleCrash()
	defer c.Queue.ShutDown()

	c.Logger.Info("Starting workers")
	defer c.Logger.Info("Stopping workers")

	workerCount.WithLabelValues(c.Name).Set(float64(numThreads))

	for i := 0; i < numThreads; i++ {
		go wait.UntilWithContext(ctx, c.startWorker, time.Second)
	}

	<-ctx.Done()
}

func (c *Controller) startWorker(ctx context.Context) {
	for c.processNextWorkItem(ctx) {
	}
}

func (c *Controller) processNextWorkItem(ctx context.Context) bool {
	// Wait until there is a new item in the working queue
	k, quit := c.Queue.Get()
	if quit {
		return false
	}
	key := k.(string)

	// No matter what, tell the queue we're done with this key,
	// to unblock other workers.
	defer c.Queue.Done(key)

	activeWorkers.WithLabelValues(c.Name).Add(1)
	defer activeWorkers.WithLabelValues(c.Name).Add(-1)

	start := time.Now()
	reconcileTime.WithLabelValues(c.Name).Observe(time.Since(start).Seconds())

	err := c.Process(ctx, key)

	// Reconcile worked, nothing else to do for this work-queue item
	if err == nil {
		c.Queue.Forget(key)
		reconcileTotal.WithLabelValues(c.Name, labelSuccess).Inc()
		return true
	}

	reconcileErrors.WithLabelValues(c.Name).Inc()
	reconcileTotal.WithLabelValues(c.Name, labelError).Inc()

	// Re-enqueue up to 5 times
	n := c.Queue.NumRequeues(key)
	if n < 5 {
		c.Queue.AddRateLimited(key)
		return true
	}

	// Give up and report error elsewhere.
	c.Queue.Forget(key)
	runtime.HandleError(err)
	c.Logger.Error(err, "Dropping key after max failed retries", "key", key, "retries", n)

	return true
}
