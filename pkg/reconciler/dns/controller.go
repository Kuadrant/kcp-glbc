package dns

import (
	"context"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/kcp-dev/apimachinery/pkg/logicalcluster"

	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/clientset/versioned"
	"github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/informers/externalversions"
	kuadrantv1lister "github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/listers/kuadrant/v1"
	"github.com/kuadrant/kcp-glbc/pkg/dns"
	awsdns "github.com/kuadrant/kcp-glbc/pkg/dns/aws"
	"github.com/kuadrant/kcp-glbc/pkg/reconciler"
)

const controllerName = "kcp-glbc-dns"

// NewController returns a new Controller which reconciles DNSRecord.
func NewController(config *ControllerConfig) (*Controller, error) {
	queue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), controllerName)

	c := &Controller{
		Controller:            reconciler.NewController(controllerName, queue),
		dnsRecordClient:       config.DnsRecordClient,
		sharedInformerFactory: config.SharedInformerFactory,
	}
	c.Process = c.process

	dnsProvider, err := createDNSProvider(*config.DNSProvider)
	if err != nil {
		return nil, err
	}
	c.dnsProvider = dnsProvider

	var dnsZones []v1.DNSZone
	zoneID, zoneIDSet := os.LookupEnv("AWS_DNS_PUBLIC_ZONE_ID")
	if zoneIDSet {
		dnsZone := &v1.DNSZone{
			ID: zoneID,
		}
		dnsZones = append(dnsZones, *dnsZone)
		klog.Infof("Using aws dns zone id : %s", zoneID)
	} else {
		klog.Warningf("No aws dns zone id set(AWS_DNS_PUBLIC_ZONE_ID). No DNS records will be created!!")
	}
	c.dnsZones = dnsZones

	// Watch for events related to DNSRecords
	c.sharedInformerFactory.Kuadrant().V1().DNSRecords().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { c.Enqueue(obj) },
		UpdateFunc: func(_, obj interface{}) { c.Enqueue(obj) },
		DeleteFunc: func(obj interface{}) { c.Enqueue(obj) },
	})

	c.indexer = c.sharedInformerFactory.Kuadrant().V1().DNSRecords().Informer().GetIndexer()
	c.lister = c.sharedInformerFactory.Kuadrant().V1().DNSRecords().Lister()

	return c, nil
}

type ControllerConfig struct {
	DnsRecordClient       kuadrantv1.ClusterInterface
	SharedInformerFactory externalversions.SharedInformerFactory
	DNSProvider           *string
}

type Controller struct {
	*reconciler.Controller
	sharedInformerFactory externalversions.SharedInformerFactory
	dnsRecordClient       kuadrantv1.ClusterInterface
	indexer               cache.Indexer
	lister                kuadrantv1lister.DNSRecordLister
	dnsProvider           dns.Provider
	dnsZones              []v1.DNSZone
}

func (c *Controller) process(ctx context.Context, key string) error {
	obj, exists, err := c.indexer.GetByKey(key)
	if err != nil {
		return err
	}

	if !exists {
		klog.Infof("Object with key %q was deleted", key)
		return nil
	}

	current := obj.(*v1.DNSRecord)

	previous := current.DeepCopy()

	if err := c.reconcile(ctx, current); err != nil {
		return err
	}

	// If the object being reconciled changed as a result, update it.
	if !equality.Semantic.DeepEqual(previous, current) {
		_, err := c.dnsRecordClient.Cluster(logicalcluster.From(current)).KuadrantV1().DNSRecords(current.Namespace).Update(ctx, current, metav1.UpdateOptions{})
		return err
	}

	return err
}

func createDNSProvider(dnsProviderName string) (dns.Provider, error) {
	var dnsProvider dns.Provider
	var dnsError error
	switch dnsProviderName {
	case "aws":
		klog.Infof("Using aws dns provider")
		dnsProvider, dnsError = newAWSDNSProvider()
	default:
		klog.Infof("Using fake dns provider")
		dnsProvider = &dns.FakeProvider{}
	}
	return dnsProvider, dnsError
}

func newAWSDNSProvider() (dns.Provider, error) {
	var dnsProvider dns.Provider
	provider, err := awsdns.NewProvider(awsdns.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS DNS manager: %v", err)
	}
	dnsProvider = provider

	return dnsProvider, nil
}
