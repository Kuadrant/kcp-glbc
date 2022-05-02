package dns

import (
	"context"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

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

	dnsProvider, err := c.createDNSProvider(config.DNSProvider)
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
		c.Logger.Info("Using AWS DNS zone", "id", zoneID)
	} else {
		c.Logger.Info("No AWS DNS zone id set (AWS_DNS_PUBLIC_ZONE_ID), no DNS records will be created!")
	}
	c.dnsZones = dnsZones

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
	DNSProvider           string
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
	dnsRecord, exists, err := c.indexer.GetByKey(key)
	if err != nil {
		return err
	}

	if !exists {
		c.Logger.Info("DNSRecord was deleted", "key", key)
		return nil
	}

	current := dnsRecord.(*v1.DNSRecord)
	previous := current.DeepCopy()

	if err = c.reconcile(ctx, current); err != nil {
		return err
	}

	if !equality.Semantic.DeepEqual(previous, current) {
		_, err := c.dnsRecordClient.Cluster(logicalcluster.From(current)).KuadrantV1().DNSRecords(current.Namespace).Update(ctx, current, metav1.UpdateOptions{})
		return err
	}

	return nil
}

func (c *Controller) createDNSProvider(dnsProviderName string) (dns.Provider, error) {
	var dnsProvider dns.Provider
	var dnsError error
	switch dnsProviderName {
	case "aws":
		c.Logger.Info("Creating DNS provider", "provider", "aws")
		dnsProvider, dnsError = newAWSDNSProvider()
	default:
		c.Logger.Info("Creating DNS provider", "provider", "fake")
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
