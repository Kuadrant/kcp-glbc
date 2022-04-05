package service

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kuadrant/kcp-glbc/pkg/util/deleteDelay"
	"github.com/kuadrant/kcp-glbc/pkg/util/metadata"
)

const (
	PlacementAnnotationName = "kcp.dev/placement"

	// PBrookes TODO deduplicate these somewhere
	clusterLabel = "kcp.dev/cluster"
	ownedByLabel = "kcp.dev/owned-by"

	shadowCleanupFinalizer = "kcp.dev/shadow-cleanup"
)

func (c *Controller) reconcile(ctx context.Context, service *corev1.Service) error {
	// route the service to the correct handler function
	_, isShadow := GetLocations(service)

	if service.DeletionTimestamp != nil {
		if isShadow {
			return c.reconcileShadowDelete(ctx, service)
		}
		return c.reconcileRootDelete(ctx, service)
	}

	if isShadow {
		return nil
	}

	return c.ReconcileRootService(ctx, service)
}

func (c *Controller) ReconcileRootService(ctx context.Context, service *corev1.Service) error {
	klog.Infof("reconciling root service: %v", service.Name)
	// create or update shadow services
	shadowCopies, err := desiredServices(service)
	if err != nil {
		return err
	}

	metadata.AddFinalizer(service, shadowCleanupFinalizer)
	// create or update desired shadows
	for _, shadow := range shadowCopies {
		klog.Infof("creating shadow service %v", shadow.Name)
		key, err := cache.MetaNamespaceKeyFunc(shadow)
		if err != nil {
			return err
		}
		obj, exists, err := c.indexer.GetByKey(key)
		if err != nil {
			return err
		}
		if exists {
			existingShadow := obj.(*corev1.Service)
			// Set the resourceVersion and UID to update the desired shadow.
			shadow.ResourceVersion = existingShadow.ResourceVersion
			shadow.UID = existingShadow.UID

			if _, err := c.coreClient.Cluster(shadow.ClusterName).CoreV1().Services(shadow.Namespace).Update(ctx, shadow, metav1.UpdateOptions{}); err != nil {
				return err
			}
		} else {
			if _, err := c.coreClient.Cluster(shadow.ClusterName).CoreV1().Services(shadow.Namespace).Create(ctx, shadow, metav1.CreateOptions{}); err != nil {
				return err
			}
		}
	}

	// find and remove undesired shadows
	current, err := c.findCurrentShadows(service)
	if err != nil {
		return err
	}

	undesiredShadows := undesiredShadowServices(current, shadowCopies)
	klog.Infof("found undesired shadows: %+v", undesiredShadows)
	for _, undesired := range undesiredShadows {
		klog.Infof("Deleting non desired service shadow %q for root: %q", undesired.Name, service.Name)
		obj, err := deleteDelay.SetDefaultDeleteAt(undesired, c.queue)
		if err != nil {
			return err
		}
		undesired = obj.(*corev1.Service)
		undesired, err := c.coreClient.Cluster(undesired.ClusterName).CoreV1().Services(undesired.Namespace).Update(ctx, undesired, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		err = c.coreClient.Cluster(undesired.ClusterName).CoreV1().Services(undesired.Namespace).Delete(ctx, undesired.Name, metav1.DeleteOptions{})
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) reconcileRootDelete(ctx context.Context, service *corev1.Service) error {
	klog.Infof("reconciling deleting root service: %v", service.Name)
	// delete undesired services
	current, err := c.findCurrentShadows(service)
	if err != nil {
		return err
	}

	for _, undesired := range current {
		klog.Infof("Deleting service shadow %q for expired root: %q", undesired.Name, service.Name)
		deleteDelay.CleanForDeletion(undesired)
		undesired, err = c.coreClient.Cluster(undesired.ClusterName).CoreV1().Services(undesired.Namespace).Update(ctx, undesired, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		if err := c.coreClient.Cluster(undesired.ClusterName).CoreV1().Services(undesired.Namespace).Delete(ctx, undesired.Name, metav1.DeleteOptions{}); err != nil {
			return err
		}
	}
	metadata.RemoveFinalizer(service, shadowCleanupFinalizer)

	return nil
}

func (c *Controller) reconcileShadowDelete(_ context.Context, service *corev1.Service) error {
	if !deleteDelay.CanDelete(service) {
		if err := deleteDelay.Requeue(service, c.queue); err != nil {
			return err
		}
		return nil
	}
	deleteDelay.CleanForDeletion(service)

	metadata.RemoveFinalizer(service, shadowCleanupFinalizer)
	return nil
}

func (c *Controller) findCurrentShadows(root *corev1.Service) ([]*corev1.Service, error) {
	// Get the current service shadows
	sel, err := labels.Parse(fmt.Sprintf("%s=%s", ownedByLabel, root.Name))
	if err != nil {
		return nil, err
	}
	klog.Infof("looking for services with label: %v", fmt.Sprintf("%s=%s", ownedByLabel, root.Name))
	return c.serviceLister.Services(root.Namespace).List(sel)
}

func desiredServices(service *corev1.Service) ([]*corev1.Service, error) {
	locations, _ := GetLocations(service)

	desiredServices := make([]*corev1.Service, 0, len(locations))

	for _, loc := range locations {
		desired := service.DeepCopy()

		// clean up clone
		delete(desired.Annotations, PlacementAnnotationName)
		desired.Finalizers = []string{}
		desired.SetResourceVersion("")
		desired.OwnerReferences = []metav1.OwnerReference{}
		desired.Name = fmt.Sprintf("%s--%s", service.Name, loc)

		if desired.Labels == nil {
			desired.Labels = map[string]string{}
		}
		desired.Labels[clusterLabel] = loc
		desired.Labels[ownedByLabel] = service.Name

		desiredServices = append(desiredServices, desired)
	}
	return desiredServices, nil
}

func undesiredShadowServices(current, desired []*corev1.Service) []*corev1.Service {
	var missing []*corev1.Service
	for _, c := range current {
		found := false
		for _, d := range desired {
			if c.Name == d.Name {
				found = true
			}
		}
		if !found {
			missing = append(missing, c)
		}
	}
	return missing
}

func GetLocations(service *corev1.Service) ([]string, bool) {
	locations := strings.Split(service.Annotations[PlacementAnnotationName], ",")
	if len(locations) == 1 && locations[0] == "" {
		return locations, true
	}
	return locations, false
}

func IsRootService(service *corev1.Service) bool {
	_, ok := service.Annotations[PlacementAnnotationName]
	return ok
}
