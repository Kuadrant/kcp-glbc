package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"github.com/rs/xid"

	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/pointer"

	"github.com/kcp-dev/logicalcluster"

	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	"github.com/kuadrant/kcp-glbc/pkg/cluster"
	"github.com/kuadrant/kcp-glbc/pkg/dns/aws"
	"github.com/kuadrant/kcp-glbc/pkg/util/metadata"
	"github.com/kuadrant/kcp-glbc/pkg/util/slice"
)

const (
	manager                  = "kcp-ingress"
	cascadeCleanupFinalizer  = "kcp.dev/cascade-cleanup"
	GeneratedRulesAnnotation = "kuadrant.dev/custom-hosts.generated"
)

type Pending struct {
	Rules []networkingv1.IngressRule `json:"rules"`
}

func (c *Controller) reconcile(ctx context.Context, ingress *networkingv1.Ingress) error {
	if ingress.DeletionTimestamp != nil && !ingress.DeletionTimestamp.IsZero() {
		c.Logger.Info("Deleting Ingress", "ingress", ingress)

		// delete any DNS records
		if err := c.ensureDNS(ctx, ingress); err != nil {
			return err
		}
		// delete any certificates
		if err := c.ensureCertificate(ctx, ingress); err != nil {
			return err
		}

		metadata.RemoveFinalizer(ingress, cascadeCleanupFinalizer)

		c.hostsWatcher.StopWatching(ingressKey(ingress), "")

		return nil
	}
	metadata.AddFinalizer(ingress, cascadeCleanupFinalizer)

	// Let's assign it a global hostname if any
	if ingress.Annotations == nil {
		ingress.Annotations = make(map[string]string)
	}
	if _, ok := ingress.Annotations[cluster.ANNOTATION_HCG_HOST]; !ok {
		ingress.Annotations[cluster.ANNOTATION_HCG_HOST] = fmt.Sprintf("%s.%s", xid.New(), c.domain)
		// Return to update the Ingress with the host annotation atomically, so it's always taken into account
		// for the TLS certificate creation.
		return nil
	}

	// if custom hosts are not enabled all the hosts in the ingress
	// will be replaced to the generated host
	customHostsLogic := c.replaceCustomHosts
	if c.customHostsEnabled {
		customHostsLogic = c.processCustomHostValidation
	}
	if err := customHostsLogic(ctx, ingress); err != nil {
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
func (c *Controller) ensureCertificate(ctx context.Context, ingress *networkingv1.Ingress) error {
	if c.certProvider == nil {
		c.Logger.Info("TLS support is not enabled, skipping certificate request")
		return nil
	}

	controlClusterContext, err := cluster.NewControlObjectMapper(ingress)
	if err != nil {
		return err
	}
	if ingress.DeletionTimestamp != nil && !ingress.DeletionTimestamp.IsZero() {
		if err := c.certProvider.Delete(ctx, controlClusterContext); err != nil {
			return err
		}
		return nil
	}
	err = c.certProvider.Create(ctx, controlClusterContext)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	upsertTLS(ingress, controlClusterContext.Host(), controlClusterContext.Name())

	return nil
}

func upsertTLS(ingress *networkingv1.Ingress, host, secretName string) {
	for i, tls := range ingress.Spec.TLS {
		if slice.ContainsString(tls.Hosts, host) {
			ingress.Spec.TLS[i] = networkingv1.IngressTLS{
				Hosts:      []string{host},
				SecretName: secretName,
			}
			return
		}
	}
	ingress.Spec.TLS = append(ingress.Spec.TLS, networkingv1.IngressTLS{
		Hosts:      []string{host},
		SecretName: secretName,
	})
}

func (c *Controller) ensureDNS(ctx context.Context, ingress *networkingv1.Ingress) error {
	if ingress.DeletionTimestamp != nil && !ingress.DeletionTimestamp.IsZero() {
		// delete DNSRecord
		err := c.kuadrantClient.Cluster(logicalcluster.From(ingress)).KuadrantV1().DNSRecords(ingress.Namespace).Delete(ctx, ingress.Name, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		return nil
	}

	if len(ingress.Status.LoadBalancer.Ingress) > 0 {
		key := ingressKey(ingress)
		var activeHosts []string
		// Start watching for address changes in the LBs hostnames
		for _, lbs := range ingress.Status.LoadBalancer.Ingress {
			if lbs.Hostname != "" {
				c.hostsWatcher.StartWatching(ctx, key, lbs.Hostname)
				activeHosts = append(activeHosts, lbs.Hostname)
			}
		}

		hostRecordWatchers := c.hostsWatcher.ListHostRecordWatchers(key)
		for _, watcher := range hostRecordWatchers {
			if !slice.ContainsString(activeHosts, watcher.Host) {
				c.hostsWatcher.StopWatching(key, watcher.Host)
			}
		}

		// Attempt to retrieve the existing DNSRecord for this Ingress
		existing, err := c.kuadrantClient.Cluster(logicalcluster.From(ingress)).KuadrantV1().DNSRecords(ingress.Namespace).Get(ctx, ingress.Name, metav1.GetOptions{})
		// If it doesn't exist, create it
		if err != nil && apierrors.IsNotFound(err) {
			// Create the DNSRecord object
			record := &v1.DNSRecord{}
			if err := c.setDnsRecordFromIngress(ctx, ingress, record); err != nil {
				return err
			}
			// Create the resource in the cluster
			existing, err = c.kuadrantClient.Cluster(logicalcluster.From(record)).KuadrantV1().DNSRecords(record.Namespace).Create(ctx, record, metav1.CreateOptions{})
			if err != nil {
				return err
			}

			// metric to observe the ingress admission time
			ingressObjectTimeToAdmission.Observe(existing.CreationTimestamp.Time.Sub(ingress.CreationTimestamp.Time).Seconds())
		} else if err == nil {
			// If it does exist, update it
			if err := c.setDnsRecordFromIngress(ctx, ingress, existing); err != nil {
				return err
			}

			data, err := json.Marshal(existing)
			if err != nil {
				return err
			}
			_, err = c.kuadrantClient.Cluster(logicalcluster.From(existing)).KuadrantV1().DNSRecords(existing.Namespace).Patch(ctx, existing.Name, types.ApplyPatchType, data, metav1.PatchOptions{FieldManager: manager, Force: pointer.Bool(true)})
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

	var newEndpoints []*v1.Endpoint

	for _, ingressTargets := range targets {
		for _, target := range ingressTargets {
			var endpoint *v1.Endpoint
			ok := false

			// If the endpoint for this target does not exist, add a new one
			if endpoint, ok = currentEndpoints[target]; !ok {
				endpoint = &v1.Endpoint{
					SetIdentifier: target,
				}
			}

			newEndpoints = append(newEndpoints, endpoint)

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

// targetsFromIngressStatus returns a map of all the IPs associated with a single ingress(cluster)
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

func ingressKey(ingress *networkingv1.Ingress) interface{} {
	key, _ := cache.MetaNamespaceKeyFunc(ingress)
	return cache.ExplicitKey(key)
}

func (c *Controller) replaceCustomHosts(_ context.Context, ingress *networkingv1.Ingress) error {
	if ingress.Annotations == nil {
		ingress.Annotations = map[string]string{}
	}

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
	}

	return nil
}

func (c *Controller) processCustomHostValidation(ctx context.Context, ingress *networkingv1.Ingress) error {
	dvs, err := c.kuadrantClient.Cluster(logicalcluster.From(ingress)).KuadrantV1().DomainVerifications().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	if err != nil {
		return err
	}

	return doProcessCustomHostValidation(c.Logger, dvs, ingress)
}

func doProcessCustomHostValidation(logger logr.Logger, dvs *v1.DomainVerificationList, ingress *networkingv1.Ingress) error {
	if ingress.Annotations == nil {
		ingress.Annotations = map[string]string{}
	}

	// Ensure the custom hosts replaced annotation is deleted, in case
	// the custom hosts feature was previously disabled
	delete(ingress.Annotations, cluster.ANNOTATION_HCG_CUSTOM_HOST_REPLACED)

	generatedHost, ok := ingress.Annotations[cluster.ANNOTATION_HCG_HOST]
	if !ok || generatedHost == "" {
		return fmt.Errorf("generated host is empty for ingress: '%v/%v'", ingress.Namespace, ingress.Name)
	}

	var hosts []string

	preservedRules := make([]networkingv1.IngressRule, 0)

	// map[Custom domain] => Index of the generated rule for the domain
	generatedRules := map[string]int{}
	i := 0

	currentGeneratedRules := map[string]int{}
	// If the annotation has already been set, start by preserving the
	// current generated rules
	if annotationValue, ok := ingress.Annotations[GeneratedRulesAnnotation]; ok {
		if err := json.Unmarshal([]byte(annotationValue), &currentGeneratedRules); err != nil {
			return err
		}

		for host, ruleIndex := range currentGeneratedRules {
			rule := ingress.Spec.Rules[ruleIndex].DeepCopy()
			preservedRules = append(preservedRules, *rule)
			generatedRules[host] = i
			i++
		}
	}

	// Create a generated rule from each custom domain
	for _, rule := range ingress.Spec.Rules {
		// ignore any rules for generated hosts (these are recalculated later)
		if rule.Host == generatedHost {
			continue
		}

		dv := findDomainVerification(ingress, rule.Host, dvs.Items)

		// check against domainverification status
		if dv != nil && dv.Status.Verified {
			preservedRules = append(preservedRules, rule)
			i++
		} else if strings.TrimSpace(rule.Host) != "" {
			hosts = append(hosts, rule.Host)
		}

		// if the host already has a generated rule, skip it
		if _, ok := generatedRules[rule.Host]; ok {
			continue
		}

		// Duplicate the rule and keep the association host => index
		generatedHostRule := *rule.DeepCopy()
		generatedHostRule.Host = generatedHost
		preservedRules = append(preservedRules, generatedHostRule)
		generatedRules[rule.Host] = i
		i++
	}
	ingress.Spec.Rules = preservedRules

	// Save the generated rules association in the annotation
	generatedHosts, err := json.Marshal(generatedRules)
	if err != nil {
		return err
	}
	ingress.Annotations[GeneratedRulesAnnotation] = string(generatedHosts)

	// Ensure that every custom domain that has been verified is preserved
GeneratedRulesLoop:
	for host, generatedIndex := range generatedRules {
		// Validate the index hasn't been corrupted
		if generatedIndex < 0 || generatedIndex >= len(ingress.Spec.Rules) {
			logger.Info(fmt.Sprintf("invalid index for domain %s in %s annotation", host, GeneratedRulesAnnotation))
			continue
		}

		// If the domain hasn't been verified, do not include the rule
		dv := findDomainVerification(ingress, host, dvs.Items)
		if dv == nil || !dv.Status.Verified {
			continue
		}

		// If the rule already has been included, skip it
		for _, rule := range ingress.Spec.Rules {
			if rule.Host == host {
				continue GeneratedRulesLoop
			}
		}

		// Create a copy of the generated rule and set the custom host
		generatedRule := ingress.Spec.Rules[generatedIndex]
		customDomainRule := generatedRule.DeepCopy()
		customDomainRule.Host = host

		ingress.Spec.Rules = append(ingress.Spec.Rules, *customDomainRule)
	}

	// clean up replaced hosts from the tls list
	removeHostsFromTLS(hosts, ingress)

	return nil
}

func findDomainVerification(ingress *networkingv1.Ingress, host string, dvs []v1.DomainVerification) *v1.DomainVerification {
	if strings.TrimSpace(host) == "" {
		return nil
	}

	for _, dv := range dvs {
		if hostMatches(host, dv.Spec.Domain) {
			return &dv
		}
	}

	return nil
}

func hostMatches(host, domain string) bool {
	parentHostParts := strings.SplitN(host, ".", 2)

	if len(parentHostParts) < 2 {
		return false
	}

	if parentHostParts[1] == domain {
		return true
	}

	return hostMatches(parentHostParts[1], domain)
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
