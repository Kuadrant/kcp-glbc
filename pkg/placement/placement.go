package placement

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"

	kcp "github.com/kcp-dev/kcp/pkg/reconciler/workload/namespace"
)

type Placer interface {
	PlaceRoutingObj([]*corev1.Service, runtime.Object) error
}

func NewPlacer() *Placement {
	return &Placement{}
}

type Placement struct {
}

// PlaceRoutingObj will ensure the right placement label is on an routing object such as ingress.
// Note this may not be needed long term (https://github.com/kcp-dev/kcp/issues/896)
func (p *Placement) PlaceRoutingObj(services []*corev1.Service, obj runtime.Object) error {
	placementValue, err := p.place(services)
	if err != nil {
		return err
	}
	if placementValue == "" {
		// nothing to do service has not been placed yet
		return nil
	}
	ac := meta.NewAccessor()
	existingLabels, err := ac.Labels(obj)
	if err != nil {
		return err
	}
	if existingLabels == nil {
		existingLabels = map[string]string{}
	}
	existingLabels[kcp.ClusterLabel] = placementValue
	return ac.SetLabels(obj, existingLabels)
}

func (p *Placement) place(services []*corev1.Service) (string, error) {
	if len(services) == 0 {
		// cant do anything here
		return "", fmt.Errorf("cannot place ingress, there are no services associated with it")
	}
	// if there is more than one service for this ingress check they are both placed on the same cluster. If not we cannot currently send the ingress to two clusters so error out (will be solved by the location API)
	var clusterLoc string

	for _, s := range services {
		if !hasClusterLabel(s.Labels) {
			continue
		}
		currentLoc := s.Labels[kcp.ClusterLabel]
		if clusterLoc != "" && currentLoc != clusterLoc {
			return "", fmt.Errorf("cannot place ingress. multiple services detected with different cluster labels")
		}
		clusterLoc = s.Labels[kcp.ClusterLabel]
	}

	return clusterLoc, nil
}

func hasClusterLabel(labels map[string]string) bool {
	if labels == nil {
		return false
	}
	val, ok := labels[kcp.ClusterLabel]
	if !ok || val == "" {
		return false
	}
	return true
}
