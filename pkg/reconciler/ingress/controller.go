package ingress

import (
	"context"

	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	networkingv1lister "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
	"github.com/kcp-dev/logicalcluster"

	certman "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1"
	certmaninformer "github.com/jetstack/cert-manager/pkg/client/informers/externalversions"
	certmanlister "github.com/jetstack/cert-manager/pkg/client/listers/certmanager/v1"
	kuadrantclientv1 "github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/clientset/versioned"
	dnsrecordinformer "github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/informers/externalversions"
	"github.com/kuadrant/kcp-glbc/pkg/net"
	basereconciler "github.com/kuadrant/kcp-glbc/pkg/reconciler"
	"github.com/kuadrant/kcp-glbc/pkg/tls"
)

const (
	controllerName                      = "kcp-glbc-ingress"
	annotationIngressKey                = "kuadarant.dev/ingress-key"
	annotationCertificateState          = "kuadrant.dev/certificate-status"
	ANNOTATION_HCG_HOST                 = "kuadrant.dev/host.generated"
	ANNOTATION_HEALTH_CHECK_PREFIX      = "kuadrant.experimental/health-"
	ANNOTATION_HCG_CUSTOM_HOST_REPLACED = "kuadrant.dev/custom-hosts.replaced"
	LABEL_HCG_MANAGED                   = "kuadrant.dev/hcg.managed"
)

// NewController returns a new Controller which reconciles Ingress.
func NewController(config *ControllerConfig) *Controller {
	queue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), controllerName)

	hostResolver := config.HostResolver
	switch impl := hostResolver.(type) {
	case *net.ConfigMapHostResolver:
		impl.Client = config.KubeClient.Cluster(tenancyv1alpha1.RootCluster)
	}
	hostResolver = net.NewSafeHostResolver(hostResolver)

	base := basereconciler.NewController(controllerName, queue)
	c := &Controller{
		Controller:               base,
		kubeClient:               config.KubeClient,
		certProvider:             config.CertProvider,
		sharedInformerFactory:    config.KCPSharedInformerFactory,
		glbcInformerFactory:      config.GlbcInformerFactory,
		dnsRecordClient:          config.DnsRecordClient,
		domain:                   config.Domain,
		hostResolver:             hostResolver,
		hostsWatcher:             net.NewHostsWatcher(&base.Logger, hostResolver, net.DefaultInterval),
		customHostsEnabled:       config.CustomHostsEnabled,
		certInformerFactory:      config.CertificateInformer,
		dnsRecordInformerFactory: config.DNSRecordInformer,
	}
	c.Process = c.process
	c.hostsWatcher.OnChange = c.Enqueue
	c.certificateLister = c.certInformerFactory.Certmanager().V1().Certificates().Lister()
	c.indexer = c.sharedInformerFactory.Networking().V1().Ingresses().Informer().GetIndexer()
	c.ingressLister = c.sharedInformerFactory.Networking().V1().Ingresses().Lister()

	// Watch for events related to Ingresses
	c.sharedInformerFactory.Networking().V1().Ingresses().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ingress := obj.(*networkingv1.Ingress)
			c.Logger.V(3).Info("enqueue ingress new ingress added", "cluster", ingress.ClusterName, "namespace", ingress.Namespace, "name", ingress.Name)
			ingressObjectTotal.Inc()
			c.Enqueue(obj)
		},
		UpdateFunc: func(old, obj interface{}) {
			if old.(metav1.Object).GetResourceVersion() != obj.(metav1.Object).GetResourceVersion() {
				ingress := obj.(*networkingv1.Ingress)
				c.Logger.V(3).Info("enqueue ingress ingress updated", "cluster", ingress.ClusterName, "namespace", ingress.Namespace, "name", ingress.Name)
				c.Enqueue(obj)
			}
		},
		DeleteFunc: func(obj interface{}) {
			ingress := obj.(*networkingv1.Ingress)
			c.Logger.V(3).Info("enqueue ingress deleted ", "cluster", ingress.ClusterName, "namespace", ingress.Namespace, "name", ingress.Name)
			ingressObjectTotal.Dec()
			c.Enqueue(obj)
		},
	})

	// watch for certificates being addded and updated
	c.certInformerFactory.Certmanager().V1().Certificates().Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			certificate, ok := obj.(*certman.Certificate)
			if !ok {
				return false
			}
			if certificate.Labels == nil {
				return false
			}
			if _, ok := certificate.Labels[LABEL_HCG_MANAGED]; !ok {
				return false
			}
			if _, ok := certificate.Annotations[annotationIngressKey]; ok {
				return true
			}
			return true
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				certificate := obj.(*certman.Certificate)
				certificateAddedHandler(certificate)
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				oldCert := oldObj.(*certman.Certificate)
				newCert := newObj.(*certman.Certificate)
				if oldCert.ResourceVersion == newCert.ResourceVersion {
					return
				}

				enq := certificateUpdatedHandler(oldCert, newCert)
				if enq == enqueue(true) {

					ingressKey := newCert.Annotations[annotationIngressKey]
					c.Logger.V(3).Info("reqeuing ingress certificate updated", "certificate", newCert.Name, "ingresskey", ingressKey)
					c.enqueueIngressByKey(ingressKey)
				}
			},
			DeleteFunc: func(obj interface{}) {
				certificate := obj.(*certman.Certificate)
				// handle metric requeue ingress if the cert is deleted and the ingress still exists
				// covers a manual deletion of cert and will ensure a new cert is created
				certificateDeletedHandler(certificate)
				ingressKey := certificate.Annotations[annotationIngressKey]
				c.Logger.V(3).Info("reqeuing ingress certificate deleted", "certificate", certificate.Name, "ingresskey", ingressKey)
				c.enqueueIngressByKey(ingressKey)
			},
		},
	})

	// watch for secrets in glbc namespace.
	// increment secret metric enqueue ingress
	c.glbcInformerFactory.Core().V1().Secrets().Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: certificateSecretFilter,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				secret := obj.(*corev1.Secret)
				issuer := secret.Annotations[tls.TlsIssuerAnnotation]
				tlsCertificateSecretCount.WithLabelValues(issuer).Inc()
				ingressKey := secret.Annotations[annotationIngressKey]
				c.Logger.V(3).Info("reqeuing ingress certificate tls secret created", "secret", secret.Name, "ingresskey", ingressKey)
				c.enqueueIngressByKey(ingressKey)
			},
			UpdateFunc: func(old, obj interface{}) {
				newSecret := obj.(*corev1.Secret)
				oldSecret := obj.(*corev1.Secret)
				if oldSecret.ResourceVersion != newSecret.ResourceVersion {
					// we only care if the secret data changed
					if !equality.Semantic.DeepEqual(oldSecret.Data, newSecret.Data) {
						ingressKey := newSecret.Annotations[annotationIngressKey]
						c.Logger.V(3).Info("reqeuing ingress certificate tls secret updated", "secret", newSecret.Name, "ingresskey", ingressKey)
						c.enqueueIngressByKey(ingressKey)
					}
				}
			},
			DeleteFunc: func(obj interface{}) {
				secret := obj.(*corev1.Secret)
				issuer := secret.Annotations[tls.TlsIssuerAnnotation]
				tlsCertificateSecretCount.WithLabelValues(issuer).Dec()
				ingressKey := secret.Annotations[annotationIngressKey]
				c.Logger.V(3).Info("reqeuing ingress certificate tls secret deleted", "secret", secret.Name, "ingresskey", ingressKey)
				c.enqueueIngressByKey(ingressKey)
			},
		},
	})

	//watch for DNSRecords
	c.dnsRecordInformerFactory.Kuadrant().V1().DNSRecords().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj interface{}) {
			//when a dns record is deleted we requeue the ingress (currently owner refs don't work in KCP)
			dns := obj.(*kuadrantv1.DNSRecord)
			if dns.Annotations == nil {
				return
			}
			// if we have a ingress key stored we can re queue the ingresss
			if ingressKey, ok := dns.Annotations[annotationIngressKey]; ok {
				c.Logger.V(3).Info("reqeuing ingress dns record deleted", "cluster", dns.ClusterName, "namespace", dns.Namespace, "name", dns.Name, "ingresskey", ingressKey)
				c.enqueueIngressByKey(ingressKey)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			newdns := newObj.(*kuadrantv1.DNSRecord)
			olddns := oldObj.(*kuadrantv1.DNSRecord)
			if olddns.ResourceVersion != newdns.ResourceVersion {
				ingressKey := newObj.(*kuadrantv1.DNSRecord).Annotations[annotationIngressKey]
				c.Logger.V(3).Info("reqeuing ingress dns record deleted", "cluster", newdns.ClusterName, "namespace", newdns.Namespace, "name", newdns.Name, "ingresskey", ingressKey)
				c.enqueueIngressByKey(ingressKey)
			}
		},
	})

	return c
}

type ControllerConfig struct {
	KubeClient      kubernetes.ClusterInterface
	DnsRecordClient kuadrantclientv1.ClusterInterface
	// informer for
	KCPSharedInformerFactory informers.SharedInformerFactory
	CertificateInformer      certmaninformer.SharedInformerFactory
	GlbcInformerFactory      informers.SharedInformerFactory
	DNSRecordInformer        dnsrecordinformer.SharedInformerFactory
	Domain                   string
	CertProvider             tls.Provider
	HostResolver             net.HostResolver
	CustomHostsEnabled       bool
}

type Controller struct {
	*basereconciler.Controller
	kubeClient               kubernetes.ClusterInterface
	sharedInformerFactory    informers.SharedInformerFactory
	dnsRecordClient          kuadrantclientv1.ClusterInterface
	indexer                  cache.Indexer
	ingressLister            networkingv1lister.IngressLister
	certificateLister        certmanlister.CertificateLister
	certProvider             tls.Provider
	domain                   string
	hostResolver             net.HostResolver
	hostsWatcher             *net.HostsWatcher
	customHostsEnabled       bool
	certInformerFactory      certmaninformer.SharedInformerFactory
	glbcInformerFactory      informers.SharedInformerFactory
	dnsRecordInformerFactory dnsrecordinformer.SharedInformerFactory
}

func (c *Controller) enqueueIngressByKey(key string) {
	ingress, err := c.getIngressByKey(key)
	//no need to handle not found as the ingress is gone
	if err != nil {
		if errors.IsNotFound(err) {
			return
		}
		runtime.HandleError(err)
		return
	}
	c.Enqueue(ingress)
}

func (c *Controller) process(ctx context.Context, key string) error {
	object, exists, err := c.indexer.GetByKey(key)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if !exists {
		// The Ingress has been deleted, so we remove any Ingress to Service tracking.
		return nil
	}

	current := object.(*networkingv1.Ingress)
	target := current.DeepCopy()
	err = c.reconcile(ctx, target)
	if err != nil {
		return err
	}
	if !equality.Semantic.DeepEqual(current, target) {
		c.Logger.V(3).Info("attempting update of changed ingress ", "ingress key ", key)
		_, err := c.kubeClient.Cluster(logicalcluster.From(target)).NetworkingV1().Ingresses(target.Namespace).Update(ctx, target, metav1.UpdateOptions{})
		return err
	}

	return nil
}

func (c *Controller) getIngressByKey(key string) (*networkingv1.Ingress, error) {
	i, exists, err := c.indexer.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(networkingv1.Resource("ingress"), key)
	}
	return i.(*networkingv1.Ingress), nil
}
