package service

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"github.com/kcp-dev/apimachinery/pkg/logicalcluster"

	"github.com/kuadrant/kcp-glbc/pkg/reconciler"
)

const controllerName = "kcp-glbc-service"

// NewController returns a new Controller which reconciles Service.
func NewController(config *ControllerConfig) (*Controller, error) {
	queue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), controllerName)
	c := &Controller{
		Controller:            reconciler.NewController(controllerName, queue),
		coreClient:            config.ServicesClient,
		sharedInformerFactory: config.SharedInformerFactory,
	}
	c.Process = c.process

	c.sharedInformerFactory.Core().V1().Services().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { c.Enqueue(obj) },
		UpdateFunc: func(_, obj interface{}) { c.Enqueue(obj) },
		DeleteFunc: func(obj interface{}) { c.Enqueue(obj) },
	})

	c.indexer = c.sharedInformerFactory.Core().V1().Services().Informer().GetIndexer()
	c.serviceLister = c.sharedInformerFactory.Core().V1().Services().Lister()

	return c, nil
}

type ControllerConfig struct {
	ServicesClient        kubernetes.ClusterInterface
	SharedInformerFactory informers.SharedInformerFactory
}

type Controller struct {
	*reconciler.Controller
	sharedInformerFactory informers.SharedInformerFactory
	coreClient            kubernetes.ClusterInterface
	indexer               cache.Indexer
	serviceLister         corev1listers.ServiceLister
}

func (c *Controller) process(ctx context.Context, key string) error {
	service, exists, err := c.indexer.GetByKey(key)
	if err != nil {
		return err
	}

	if !exists {
		c.Logger.Info("Service was deleted", "key", key)
		return nil
	}

	current := service.(*corev1.Service)
	previous := current.DeepCopy()

	if err = c.reconcile(ctx, current); err != nil {
		return err
	}

	if !equality.Semantic.DeepEqual(previous, current) {
		_, err := c.coreClient.Cluster(logicalcluster.From(current)).CoreV1().Services(current.Namespace).Update(ctx, current, metav1.UpdateOptions{})
		return err
	}

	return nil
}
