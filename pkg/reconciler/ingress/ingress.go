package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"

	"github.com/kcp-dev/apimachinery/pkg/logicalcluster"

	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	"github.com/kuadrant/kcp-glbc/pkg/cluster"
	"github.com/kuadrant/kcp-glbc/pkg/dns/aws"
	"github.com/kuadrant/kcp-glbc/pkg/util/metadata"
	"github.com/kuadrant/kcp-glbc/pkg/util/slice"
)

const (
	manager                 = "kcp-ingress"
	cascadeCleanupFinalizer = "kcp.dev/cascade-cleanup"
)

func (c *Controller) reconcile(ctx context.Context, ingress *networkingv1.Ingress) error {
	// is deleting
	if ingress.DeletionTimestamp != nil && !ingress.DeletionTimestamp.IsZero() {
		klog.Infof("deleting root ingress '%v'", ingress.Name)

		// delete any DNS records
		if err := c.ensureDNS(ctx, ingress); err != nil {
			return err
		}
		// delete any certificates
		if err := c.ensureCertificate(ctx, ingress); err != nil {
			return err
		}

		metadata.RemoveFinalizer(ingress, cascadeCleanupFinalizer)

		c.hostsWatcher.StopWatching(ingressKey(ingress))

		return nil
	}
	metadata.AddFinalizer(ingress, cascadeCleanupFinalizer)

	if ingress.Annotations == nil || ingress.Annotations[cluster.ANNOTATION_HCG_HOST] == "" {
		// Let's assign it a global hostname if any
		generatedHost := fmt.Sprintf("%s.%s", xid.New(), *c.domain)
		patch := fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`, cluster.ANNOTATION_HCG_HOST, generatedHost)
		i, err := c.patchIngress(ctx, ingress, []byte(patch))
		if err != nil {
			return err
		}
		ingress = i
	}

	// if custom hosts are not enabled all the hosts in the ingress
	// will be replaced to the generated host
	if !*c.customHostsEnabled {
		err := c.replaceCustomHosts(ctx, ingress)
		if err != nil {
			return err
		}
	}

	if err := c.ensurePlacement(ctx, ingress); err != nil {
		return err
	}

	// setup certificates
	if err := c.ensureCertificate(ctx, ingress); err != nil {
		return err
	}

	// update DNS
	if err := c.ensureDNS(ctx, ingress); err != nil {
		return err
	}

	return nil
}

// ensureCertificate creates a certificate request for the root ingress into the control cluster
func (c *Controller) ensureCertificate(ctx context.Context, rootIngress *networkingv1.Ingress) error {
	if !c.tlsEnabled {
		klog.Info("TLS support is not enabled, skipping certificate request")
		return nil
	}

	controlClusterContext, err := cluster.NewControlObjectMapper(rootIngress)
	if err != nil {
		return err
	}
	if rootIngress.DeletionTimestamp != nil && !rootIngress.DeletionTimestamp.IsZero() {
		if err := c.certProvider.Delete(ctx, controlClusterContext); err != nil {
			return err
		}
		return nil
	}
	err = c.certProvider.Create(ctx, controlClusterContext)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	klog.Info("Patching Ingress With TLS ", rootIngress.Name)
	patch := fmt.Sprintf(`{"spec":{"tls":[{"hosts":[%q],"secretName":%q}]}}`, controlClusterContext.Host(), controlClusterContext.Name())
	if _, err := c.patchIngress(ctx, rootIngress, []byte(patch)); err != nil {
		klog.Info("failed to patch ingress *** ", err)
		return err
	}

	return nil
}

func (c *Controller) ensureDNS(ctx context.Context, ingress *networkingv1.Ingress) error {

	if ingress.DeletionTimestamp != nil && !ingress.DeletionTimestamp.IsZero() {
		// delete DNSRecord
		err := c.dnsRecordClient.Cluster(logicalcluster.From(ingress)).KuadrantV1().DNSRecords(ingress.Namespace).Delete(ctx, ingress.Name, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
		return nil
	}

	if len(ingress.Status.LoadBalancer.Ingress) > 0 {
		var activeHosts []string
		// Start watching for address changes in the LBs hostnames
		for _, lbs := range ingress.Status.LoadBalancer.Ingress {
			if lbs.Hostname != "" {
				c.hostsWatcher.StartWatching(ctx, ingressKey(ingress), lbs.Hostname)
				activeHosts = append(activeHosts, lbs.Hostname)
			}
		}

		hostRecordWatchers := c.hostsWatcher.ListHostRecordWatchers(ingressKey(ingress))
		for _, watcher := range hostRecordWatchers {
			if !slice.ContainsString(activeHosts, watcher.Host) {
				watcher.Stop()
			}
		}

		// Attempt to retrieve the existing DNSRecord for this Ingress
		existing, err := c.dnsRecordClient.Cluster(logicalcluster.From(ingress)).KuadrantV1().DNSRecords(ingress.Namespace).Get(ctx, ingress.Name, metav1.GetOptions{})
		// If it doesn't exist, create it
		if err != nil && errors.IsNotFound(err) {
			// Create the DNSRecord object
			record := &v1.DNSRecord{}
			if err := c.setDnsRecordFromIngress(ctx, ingress, record); err != nil {
				return err
			}
			// Create the resource in the cluster
			_, err = c.dnsRecordClient.Cluster(logicalcluster.From(record)).KuadrantV1().DNSRecords(record.Namespace).Create(ctx, record, metav1.CreateOptions{})
			if err != nil {
				return err
			}
		} else if err == nil {
			// If it does exist, update it
			if err := c.setDnsRecordFromIngress(ctx, ingress, existing); err != nil {
				return err
			}

			data, err := json.Marshal(existing)
			if err != nil {
				return err
			}
			_, err = c.dnsRecordClient.Cluster(logicalcluster.From(existing)).KuadrantV1().DNSRecords(existing.Namespace).Patch(ctx, existing.Name, types.ApplyPatchType, data, metav1.PatchOptions{FieldManager: manager, Force: pointer.Bool(true)})
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	return nil
}

func (c *Controller) setDnsRecordFromIngress(ctx context.Context, ingress *networkingv1.Ingress, dnsRecord *v1.DNSRecord) error {
	dnsRecord.TypeMeta = metav1.TypeMeta{
		APIVersion: v1.SchemeGroupVersion.String(),
		Kind:       "DNSRecord",
	}
	dnsRecord.ObjectMeta = metav1.ObjectMeta{
		Name:        ingress.Name,
		Namespace:   ingress.Namespace,
		ClusterName: ingress.ClusterName,
	}

	metadata.CopyAnnotationsPredicate(ingress, dnsRecord, metadata.KeyPredicate(func(key string) bool {
		return strings.HasPrefix(key, cluster.ANNOTATION_HEALTH_CHECK_PREFIX)
	}))

	// Sets the Ingress as the owner reference
	dnsRecord.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion:         networkingv1.SchemeGroupVersion.String(),
			Kind:               "Ingress",
			Name:               ingress.Name,
			UID:                ingress.UID,
			Controller:         pointer.Bool(true),
			BlockOwnerDeletion: pointer.Bool(true),
		},
	})

	return c.setEndpointsFromIngress(ctx, ingress, dnsRecord)
}

func (c *Controller) setEndpointsFromIngress(ctx context.Context, ingress *networkingv1.Ingress, dnsRecord *v1.DNSRecord) error {
	targets, err := c.targetsFromIngressStatus(ctx, ingress.Status)
	if err != nil {
		return err
	}

	hostname := ingress.Annotations[cluster.ANNOTATION_HCG_HOST]

	// Build a map[Address]Endpoint with the current endpoints to assist
	// finding endpoints that match the targets
	currentEndpoints := make(map[string]*v1.Endpoint, len(dnsRecord.Spec.Endpoints))
	for _, endpoint := range dnsRecord.Spec.Endpoints {
		address, ok := endpoint.GetAddress()
		if !ok {
			continue
		}

		currentEndpoints[address] = endpoint
	}

	newEndpoints := make([]*v1.Endpoint, len(targets))

	for _, ingressTargets := range targets {
		for i, target := range ingressTargets {
			var endpoint *v1.Endpoint
			ok := false

			// If the endpoint for this target does not exist, add a new one
			if endpoint, ok = currentEndpoints[target]; !ok {
				endpoint = &v1.Endpoint{
					SetIdentifier: target,
				}
			}

			newEndpoints[i] = endpoint

			// Update the endpoint fields
			endpoint.DNSName = hostname
			endpoint.RecordType = "A"
			endpoint.Targets = []string{target}
			endpoint.RecordTTL = 60
			endpoint.SetProviderSpecific(aws.ProviderSpecificWeight, awsEndpointWeight(len(ingressTargets)))
		}
	}

	dnsRecord.Spec.Endpoints = newEndpoints
	return nil
}

//targetsFromIngressStatus returns a map of all the IPs associated with a single ingress(cluster)
func (c *Controller) targetsFromIngressStatus(ctx context.Context, status networkingv1.IngressStatus) (map[string][]string, error) {
	var targets = make(map[string][]string, len(status.LoadBalancer.Ingress))

	for _, lb := range status.LoadBalancer.Ingress {
		if lb.IP != "" {
			targets[lb.IP] = []string{lb.IP}
		}
		if lb.Hostname != "" {
			ips, err := c.hostResolver.LookupIPAddr(ctx, lb.Hostname)
			if err != nil {
				return nil, err
			}
			targets[lb.Hostname] = []string{}
			for _, ip := range ips {
				targets[lb.Hostname] = append(targets[lb.Hostname], ip.IP.String())
			}
		}
	}
	return targets, nil
}

// getServices will parse the ingress object and return a list of the services.
func (c *Controller) getServices(ctx context.Context, ingress *networkingv1.Ingress) ([]*corev1.Service, error) {
	var services []*corev1.Service
	for _, rule := range ingress.Spec.Rules {
		for _, path := range rule.HTTP.Paths {
			klog.Infof("getting service: %v", path.Backend.Service.Name)
			service, err := c.kubeClient.Cluster(logicalcluster.From(ingress)).CoreV1().Services(ingress.Namespace).Get(ctx, path.Backend.Service.Name, metav1.GetOptions{})
			if err == nil {
				c.tracker.add(ingress, service)
				services = append(services, service)
			} else if !errors.IsNotFound(err) {
				return nil, err
			} else {
				// ignore service not found errors
				continue
			}
		}
	}
	return services, nil
}

func (c *Controller) ensurePlacement(ctx context.Context, ingress *networkingv1.Ingress) error {
	svcs, err := c.getServices(ctx, ingress)
	if err != nil {
		return err
	}
	if err := c.ingressPlacer.PlaceRoutingObj(svcs, ingress); err != nil {
		return err
	}
	if _, err := c.kubeClient.Cluster(logicalcluster.From(ingress)).NetworkingV1().Ingresses(ingress.Namespace).Update(ctx, ingress, metav1.UpdateOptions{}); err != nil {
		return err
	}
	return nil

}

func (c *Controller) patchIngress(ctx context.Context, ingress *networkingv1.Ingress, data []byte) (*networkingv1.Ingress, error) {
	return c.kubeClient.Cluster(logicalcluster.From(ingress)).NetworkingV1().Ingresses(ingress.Namespace).
		Patch(ctx, ingress.Name, types.MergePatchType, data, metav1.PatchOptions{FieldManager: manager})
}

func ingressKey(ingress *networkingv1.Ingress) interface{} {
	key, _ := cache.MetaNamespaceKeyFunc(ingress)
	return cache.ExplicitKey(key)
}

func (c *Controller) replaceCustomHosts(ctx context.Context, ingress *networkingv1.Ingress) error {
	generatedHost := ingress.Annotations[cluster.ANNOTATION_HCG_HOST]
	var hosts []string
	for i, rule := range ingress.Spec.Rules {
		if rule.Host != generatedHost {
			ingress.Spec.Rules[i].Host = generatedHost
			hosts = append(hosts, rule.Host)
		}
	}

	// clean up replaced hosts from the tls list
	removeHostsFromTLS(hosts, ingress)

	if len(hosts) > 0 {
		ingress.Annotations[cluster.ANNOTATION_HCG_CUSTOM_HOST_REPLACED] = fmt.Sprintf(" replaced custom hosts %v to the glbc host due to custom host policy not being allowed",
			hosts)
		if _, err := c.kubeClient.Cluster(logicalcluster.From(ingress)).NetworkingV1().Ingresses(ingress.Namespace).Update(ctx, ingress, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}

	return nil
}

func removeHostsFromTLS(hostsToRemove []string, ingress *networkingv1.Ingress) {
	for _, host := range hostsToRemove {
		for i, tls := range ingress.Spec.TLS {
			hosts := tls.Hosts
			for j, ingressHost := range tls.Hosts {
				if ingressHost == host {
					hosts = append(hosts[:j], hosts[j+1:]...)
				}
			}
			// if there are no hosts remaining remove the entry for TLS
			if len(hosts) == 0 {
				ingress.Spec.TLS[i] = networkingv1.IngressTLS{}
			} else {
				ingress.Spec.TLS[i].Hosts = hosts
			}
		}
	}
}

// awsEndpointWeight returns the weight value for a single AWS record in a set of records where the traffic is split
// evenly between a number of clusters/ingresses, each splitting traffic evenly to a number of IPs (numIPs)
//
// Divides the number of IPs by a known weight allowance for a cluster/ingress, note that this means:
// * Will always return 1 after a certain number of ips is reached, 60 in the current case (maxWeight / 2)
// * Will return values that don't add up to the total maxWeight when the number of ingresses is not divisible by numIPs
//
// The aws weight value must be an integer between 0 and 255.
// https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/resource-record-sets-values-weighted.html#rrsets-values-weighted-weight
func awsEndpointWeight(numIPs int) string {
	maxWeight := 120
	if numIPs > maxWeight {
		numIPs = maxWeight
	}
	return strconv.Itoa(maxWeight / numIPs)
}
