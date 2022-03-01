package ingress

import (
	"context"
	"fmt"
	"github.com/rs/xid"
	"k8s.io/apimachinery/pkg/util/json"
	"net"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"

	v1 "github.com/kuadrant/kcp-ingress/pkg/apis/kuadrant/v1"
)

const (
	hostGeneratedAnnotation = "kuadrant.dev/host.generated"
	ownedByLabel            = "ingress.kcp.dev/owned-by-ingress"
	manager                 = "kcp-ingress"
	dnsCleanupFinalizer     = "ingress.kcp.dev/ingress-dns-cleanup"
)

func (c *Controller) reconcileLeafDNS(ctx context.Context, ingress *networkingv1.Ingress) error {
	// is deleting
	if ingress.DeletionTimestamp != nil && !ingress.DeletionTimestamp.IsZero() {
		// delete DNSRecord
		err := c.dnsRecordClient.DNSRecords(ingress.Namespace).Delete(ctx, ingress.Name, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
		removeFinalizer(ingress, dnsCleanupFinalizer)
		return nil
	}

	AddFinalizer(ingress, dnsCleanupFinalizer)
	if ingress.Annotations == nil || ingress.Annotations[hostGeneratedAnnotation] == "" {
		// Let's assign it a global hostname if any
		generatedHost := fmt.Sprintf("%s.%s", xid.New(), *c.domain)
		patch := fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`, hostGeneratedAnnotation, generatedHost)
		if err := c.patchIngress(ctx, ingress, []byte(patch)); err != nil {
			return err
		}
	}

	// Reconcile the DNSRecord for the root Ingress
	if len(ingress.Status.LoadBalancer.Ingress) > 0 {
		// The ingress has been admitted, let's expose the local load-balancing point to the global LB.
		record, err := getDNSRecord(ingress.Annotations[hostGeneratedAnnotation], ingress)
		if err != nil {
			return err
		}
		_, err = c.dnsRecordClient.DNSRecords(record.Namespace).Create(ctx, record, metav1.CreateOptions{})
		if err != nil {
			if !errors.IsAlreadyExists(err) {
				return err
			}
			data, err := json.Marshal(record)
			if err != nil {
				return err
			}
			_, err = c.dnsRecordClient.DNSRecords(record.Namespace).Patch(ctx, record.Name, types.ApplyPatchType, data, metav1.PatchOptions{FieldManager: manager, Force: pointer.Bool(true)})
			if err != nil {
				return err
			}
		}
	} else {
		err := c.dnsRecordClient.DNSRecords(ingress.Namespace).Delete(ctx, ingress.Name, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

// TODO may want to move this to its own package in the future
func getDNSRecord(hostname string, ingress *networkingv1.Ingress) (*v1.DNSRecord, error) {
	var targets []string
	for _, lbs := range ingress.Status.LoadBalancer.Ingress {
		if lbs.Hostname != "" {
			// TODO: once we are adding tests abstract to interface
			ips, err := net.LookupIP(lbs.Hostname)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				targets = append(targets, ip.String())
			}
		}
		if lbs.IP != "" {
			targets = append(targets, lbs.IP)
		}
	}

	record := &v1.DNSRecord{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1.SchemeGroupVersion.String(),
			Kind:       "DNSRecord",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ingress.Namespace,
			Name:      ingress.Name,
		},
		Spec: v1.DNSRecordSpec{
			DNSName:    hostname,
			RecordType: "A",
			Targets:    targets,
			RecordTTL:  60,
		},
	}

	// Sets the Ingress as the owner reference
	record.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion:         networkingv1.SchemeGroupVersion.String(),
			Kind:               "Ingress",
			Name:               ingress.Name,
			UID:                ingress.UID,
			Controller:         pointer.Bool(true),
			BlockOwnerDeletion: pointer.Bool(true),
		},
	})

	return record, nil
}

func (c *Controller) patchIngress(ctx context.Context, ingress *networkingv1.Ingress, data []byte) error {
	i, err := c.client.NetworkingV1().Ingresses(ingress.Namespace).
		Patch(ctx, ingress.Name, types.MergePatchType, data, metav1.PatchOptions{FieldManager: manager})
	if err != nil {
		return err
	}
	ingress = i
	return nil
}

func getRootName(ingress *networkingv1.Ingress) (rootName string, isLeaf bool) {
	if ingress.Labels != nil {
		rootName, isLeaf = ingress.Labels[ownedByLabel]
	}

	return
}

func AddFinalizer(ingress *networkingv1.Ingress, finalizer string) {
	for _, v := range ingress.Finalizers {
		if v == finalizer {
			return
		}
	}
	ingress.Finalizers = append(ingress.Finalizers, finalizer)
}

func removeFinalizer(ingress *networkingv1.Ingress, finalizer string) {
	for i, v := range ingress.Finalizers {
		if v == finalizer {
			ingress.Finalizers[i] = ingress.Finalizers[len(ingress.Finalizers)-1]
			ingress.Finalizers = ingress.Finalizers[:len(ingress.Finalizers)-1]
			return
		}
	}
}
