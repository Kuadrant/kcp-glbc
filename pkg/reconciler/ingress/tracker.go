package ingress

import (
	"sync"

	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	k8scache "k8s.io/client-go/tools/cache"
)

// tracker is used to track the relationship between services and ingresses.
// It is used to determine which ingresses are affected by a service change and
// trigger a reconciliation of the affected ingresses.
type tracker struct {
	lock               sync.Mutex
	logger             logr.Logger
	serviceToIngresses map[string]sets.String
	ingressToServices  map[string]sets.String
}

// newTracker creates a new tracker.
func newTracker(l *logr.Logger) *tracker {
	return &tracker{
		logger:             l.WithName("tracker"),
		serviceToIngresses: make(map[string]sets.String),
		ingressToServices:  make(map[string]sets.String),
	}
}

// getIngressesForService returns the list of ingresses that are related to a given service.
func (t *tracker) getIngressesForService(key string) sets.String {
	t.lock.Lock()
	defer t.lock.Unlock()

	ingresses, ok := t.serviceToIngresses[key]
	if !ok {
		return sets.String{}
	}

	return ingresses
}

// Adds a service to an ingress (key) to be tracked.
func (t *tracker) add(ingress *networkingv1.Ingress, service *corev1.Service) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.logger.Info("Tracking Service for Ingress", "service", service, "ingress", ingress)

	ingressKey, err := k8scache.MetaNamespaceKeyFunc(ingress)
	if err != nil {
		t.logger.Error(err, "Failed to get Ingress key")
		return
	}

	serviceKey, err := k8scache.MetaNamespaceKeyFunc(service)
	if err != nil {
		t.logger.Error(err, "Failed to get Service key")
		return
	}

	if t.serviceToIngresses[serviceKey] == nil {
		t.serviceToIngresses[serviceKey] = sets.NewString()
	}

	t.serviceToIngresses[serviceKey].Insert(ingressKey)

	if t.ingressToServices[ingressKey] == nil {
		t.ingressToServices[ingressKey] = sets.NewString()
	}

	t.ingressToServices[ingressKey].Insert(serviceKey)
}

// deleteIngress deletes an ingress from all the tracked services
func (t *tracker) deleteIngress(ingressKey string) {
	t.lock.Lock()
	defer t.lock.Unlock()

	services, ok := t.ingressToServices[ingressKey]
	if !ok {
		return
	}

	for _, serviceKey := range services.List() {
		t.serviceToIngresses[serviceKey].Delete(ingressKey)
	}

	delete(t.ingressToServices, ingressKey)
}
