package deployment

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kuadrant/kcp-glbc/pkg/reconciler/service"
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

func (c *Controller) reconcile(ctx context.Context, deployment *appsv1.Deployment) error {
	if IsShadowDeployment(deployment) {
		if deployment.DeletionTimestamp != nil {
			return c.reconcileShadowDelete(ctx, deployment)
		} else {
			return nil
		}
	}

	if deployment.DeletionTimestamp != nil {
		return c.reconcileRootDelete(ctx, deployment)
	}
	return c.reconcileRoot(ctx, deployment)
}

func (c *Controller) reconcileRoot(ctx context.Context, deployment *appsv1.Deployment) error {
	// add finalizers
	metadata.AddFinalizer(deployment, shadowCleanupFinalizer)

	// find root services
	rootServices, err := c.findRootServices(deployment)
	if err != nil {
		return err
	}

	for _, rootService := range rootServices {
		// generate and reconcile required shadow deployments
		locations, _ := service.GetLocations(rootService)
		desiredShadows := generateShadowDeployments(locations, deployment)
		for _, shadow := range desiredShadows {
			klog.Infof("creating shadow deployment %v", shadow.Name)
			key, err := cache.MetaNamespaceKeyFunc(shadow)
			if err != nil {
				return err
			}
			obj, exists, err := c.indexer.GetByKey(key)
			if err != nil {
				return err
			}
			if exists {
				existingShadow := obj.(*appsv1.Deployment)
				// Set the resourceVersion and UID to update the desired shadow.
				shadow.ResourceVersion = existingShadow.ResourceVersion
				shadow.UID = existingShadow.UID

				if _, err := c.coreClient.Cluster(shadow.ClusterName).AppsV1().Deployments(shadow.Namespace).Update(ctx, shadow, metav1.UpdateOptions{}); err != nil {
					return err
				}
			} else {
				if _, err := c.coreClient.Cluster(shadow.ClusterName).AppsV1().Deployments(shadow.Namespace).Create(ctx, shadow, metav1.CreateOptions{}); err != nil {
					return err
				}
			}
		}

		// find unrequired desiredShadows and remove them
		allDeployments, err := c.findCurrentShadows(deployment)
		if err != nil {
			return err
		}
		undesiredShadows := undesiredShadowDeployments(allDeployments, desiredShadows)
		klog.Infof("found undesired deployments: %v", len(undesiredShadows))
		for _, undesired := range undesiredShadows {
			klog.Infof("Deleting non desired deployment shadow %q for root: %q", undesired.Name, deployment.Name)
			obj, err := deleteDelay.SetDefaultDeleteAt(undesired, c.queue)
			if err != nil {
				return err
			}
			undesired = obj.(*appsv1.Deployment)
			undesired, err := c.coreClient.Cluster(undesired.ClusterName).AppsV1().Deployments(undesired.Namespace).Update(ctx, undesired, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			err = c.coreClient.Cluster(undesired.ClusterName).AppsV1().Deployments(undesired.Namespace).Delete(ctx, undesired.Name, metav1.DeleteOptions{})
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Controller) reconcileRootDelete(ctx context.Context, deployment *appsv1.Deployment) error {
	// find and delete shadows
	shadows, err := c.findCurrentShadows(deployment)
	if err != nil {
		return err
	}
	for _, undesired := range shadows {
		klog.Infof("Deleting deployment shadow %q for expired root: %q", undesired.Name, deployment.Name)
		deleteDelay.CleanForDeletion(undesired)
		undesired, err = c.coreClient.Cluster(undesired.ClusterName).AppsV1().Deployments(undesired.Namespace).Update(ctx, undesired, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		if err := c.coreClient.Cluster(undesired.ClusterName).AppsV1().Deployments(undesired.Namespace).Delete(ctx, undesired.Name, metav1.DeleteOptions{}); err != nil {
			return err
		}
	}
	// remove finalizers
	metadata.RemoveFinalizer(deployment, shadowCleanupFinalizer)

	return nil
}

func (c *Controller) reconcileShadowDelete(_ context.Context, deployment *appsv1.Deployment) error {
	// honour deleteDelay finalizer
	if !deleteDelay.CanDelete(deployment) {
		if err := deleteDelay.Requeue(deployment, c.queue); err != nil {
			return err
		}
		return nil
	}
	deleteDelay.CleanForDeletion(deployment)
	return nil
}

func generateShadowDeployments(locations []string, rootDeployment *appsv1.Deployment) []*appsv1.Deployment {
	var retShadows []*appsv1.Deployment

	for _, location := range locations {

		desired := rootDeployment.DeepCopy()
		// clean up desired objects meta data
		delete(desired.Annotations, PlacementAnnotationName)
		desired.Finalizers = []string{}
		desired.SetResourceVersion("")
		desired.OwnerReferences = []metav1.OwnerReference{}
		desired.Name = fmt.Sprintf("%s-%s-%s", rootDeployment.Name, rootDeployment.Name, location)

		if desired.Labels == nil {
			desired.Labels = map[string]string{}
		}
		desired.Labels[clusterLabel] = location
		desired.Labels[ownedByLabel] = rootDeployment.Name

		retShadows = append(retShadows, desired)
	}
	return retShadows
}

func IsShadowDeployment(deployment *appsv1.Deployment) bool {
	_, ok := deployment.Labels[clusterLabel]
	return ok
}

// find any services that select the provided deployment
func (c *Controller) findRootServices(deployment *appsv1.Deployment) ([]*corev1.Service, error) {
	var retServices []*corev1.Service

	services, err := c.serviceLister.Services(deployment.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	for _, svc := range services {
		if !service.IsRootService(svc) {
			continue
		}

		if labels.SelectorFromSet(svc.Spec.Selector).Matches(labels.Set(deployment.Spec.Template.Labels)) {
			retServices = append(retServices, svc)
		}
	}
	return retServices, nil
}

func (c *Controller) findCurrentShadows(root *appsv1.Deployment) ([]*appsv1.Deployment, error) {
	// Get the current deployment shadows
	sel := labels.SelectorFromSet(labels.Set{ownedByLabel: root.Name})
	klog.Infof("looking for services with label: %v", fmt.Sprintf("%s=%s", ownedByLabel, root.Name))
	return c.deploymentLister.Deployments(root.Namespace).List(sel)
}

func undesiredShadowDeployments(current, desired []*appsv1.Deployment) []*appsv1.Deployment {
	var missing []*appsv1.Deployment
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
