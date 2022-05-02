package ingress

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	networkingv1lister "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"github.com/kcp-dev/apimachinery/pkg/logicalcluster"
	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"

	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/clientset/versioned"
	"github.com/kuadrant/kcp-glbc/pkg/net"
	"github.com/kuadrant/kcp-glbc/pkg/placement"
	"github.com/kuadrant/kcp-glbc/pkg/reconciler"
	"github.com/kuadrant/kcp-glbc/pkg/tls"
)

const controllerName = "kcp-glbc-ingress"

// NewController returns a new Controller which reconciles Ingress.
func NewController(config *ControllerConfig) *Controller {
	queue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), controllerName)

	hostResolver := config.HostResolver
	switch impl := hostResolver.(type) {
	case *net.ConfigMapHostResolver:
		impl.Client = config.KubeClient.Cluster(tenancyv1alpha1.RootCluster)
	}
	hostResolver = net.NewSafeHostResolver(hostResolver)
	ingressPlacer := placement.NewPlacer()

	base := reconciler.NewController(controllerName, queue)
	c := &Controller{
		Controller:            base,
		kubeClient:            config.KubeClient,
		certProvider:          config.CertProvider,
		sharedInformerFactory: config.SharedInformerFactory,
		dnsRecordClient:       config.DnsRecordClient,
		domain:                config.Domain,
		tracker:               newTracker(&base.Logger),
		hostResolver:          hostResolver,
		hostsWatcher:          net.NewHostsWatcher(&base.Logger, hostResolver, net.DefaultInterval),
		customHostsEnabled:    config.CustomHostsEnabled,
		ingressPlacer:         ingressPlacer,
	}
	c.Process = c.process
	c.hostsWatcher.OnChange = c.Enqueue

	// Watch for events related to Ingresses
	c.sharedInformerFactory.Networking().V1().Ingresses().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { c.Enqueue(obj) },
		UpdateFunc: func(_, obj interface{}) { c.Enqueue(obj) },
		DeleteFunc: func(obj interface{}) { c.Enqueue(obj) },
	})

	// Watch for events related to Services
	c.sharedInformerFactory.Core().V1().Services().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(_, obj interface{}) { c.ingressesFromService(obj) },
		DeleteFunc: func(obj interface{}) { c.ingressesFromService(obj) },
	})

	c.indexer = c.sharedInformerFactory.Networking().V1().Ingresses().Informer().GetIndexer()
	c.lister = c.sharedInformerFactory.Networking().V1().Ingresses().Lister()

	return c
}

type ControllerConfig struct {
	KubeClient            kubernetes.ClusterInterface
	DnsRecordClient       kuadrantv1.ClusterInterface
	SharedInformerFactory informers.SharedInformerFactory
	Domain                string
	CertProvider          tls.Provider
	HostResolver          net.HostResolver
	CustomHostsEnabled    bool
}

type Controller struct {
	*reconciler.Controller
	kubeClient            kubernetes.ClusterInterface
	sharedInformerFactory informers.SharedInformerFactory
	dnsRecordClient       kuadrantv1.ClusterInterface
	indexer               cache.Indexer
	lister                networkingv1lister.IngressLister
	certProvider          tls.Provider
	domain                string
	tracker               *tracker
	hostResolver          net.HostResolver
	hostsWatcher          *net.HostsWatcher
	customHostsEnabled    bool
	ingressPlacer         placement.Placer
}

func (c *Controller) process(ctx context.Context, key string) error {
	ingress, exists, err := c.indexer.GetByKey(key)
	if err != nil {
		return err
	}

	if !exists {
		c.Logger.Info("Ingress was deleted", "key", key)
		// The Ingress has been deleted, so we remove any Ingress to Service tracking.
		c.tracker.deleteIngress(key)
		return nil
	}

	current := ingress.(*networkingv1.Ingress)
	previous := current.DeepCopy()

	err = c.reconcile(ctx, current)
	if err != nil {
		return err
	}

	if !equality.Semantic.DeepEqual(previous, current) {
		_, err := c.kubeClient.Cluster(logicalcluster.From(current)).NetworkingV1().Ingresses(current.Namespace).Update(ctx, current, metav1.UpdateOptions{})
		return err
	}

	return nil
}

// ingressesFromService enqueues all the related ingresses for a given service when the service is changed.
func (c *Controller) ingressesFromService(obj interface{}) {
	service := obj.(*corev1.Service)

	serviceKey, err := cache.MetaNamespaceKeyFunc(service)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	// Does that Service has any Ingress associated to?
	ingresses := c.tracker.getIngressesForService(serviceKey)

	// One Service can be referenced by 0..N Ingresses, so we need to enqueue all the related Ingresses.
	for _, ingress := range ingresses.List() {
		c.Logger.Info("Enqueuing Ingress reconciliation via tracked Service", "ingress", ingress, "service", service)
		c.Queue.Add(ingress)
	}
}
