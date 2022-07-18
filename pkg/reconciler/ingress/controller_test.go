package ingress

import (
	"testing"

	"github.com/go-logr/logr"
	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	"github.com/kuadrant/kcp-glbc/pkg/reconciler"
	networkingv1 "k8s.io/api/networking/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	networkingv1lister "k8s.io/client-go/listers/networking/v1"
)

type mockLister struct {
	ingresses []*networkingv1.Ingress
}

// Get implements v1.IngressNamespaceLister
func (l *mockLister) Get(name string) (*networkingv1.Ingress, error) {
	for _, ingress := range l.ingresses {
		if ingress.Name == name {
			return ingress, nil
		}
	}

	return nil, nil
}

// Ingresses implements v1.IngressLister
func (l *mockLister) Ingresses(namespace string) networkingv1lister.IngressNamespaceLister {
	return l
}

// List implements v1.IngressLister
func (l *mockLister) List(selector labels.Selector) ([]*networkingv1.Ingress, error) {
	return l.ingresses, nil
}

var _ networkingv1lister.IngressLister = &mockLister{}
var _ networkingv1lister.IngressNamespaceLister = &mockLister{}

func TestIngressesFromDomainVerification(t *testing.T) {
	scenarios := []struct {
		Name               string
		Ingresses          []*networkingv1.Ingress
		DomainVerification kuadrantv1.DomainVerification
		ExpectedEnqueued   []string
	}{
		{
			Name: "Enqueue verified",
			Ingresses: []*networkingv1.Ingress{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "enqueued",
						Namespace: "",
						Annotations: map[string]string{
							GeneratedRulesAnnotation: `
							{"test.pb-custom.hcpapps.net": 0}
							`,
						},
					},
					Spec: networkingv1.IngressSpec{
						Rules: []networkingv1.IngressRule{
							{
								Host:             "generated",
								IngressRuleValue: networkingv1.IngressRuleValue{},
							},
						},
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "not-enqueued",
						Namespace: "",
						Annotations: map[string]string{
							GeneratedRulesAnnotation: `
							{"another.domain.net": 0}
							`,
						},
					},
					Spec: networkingv1.IngressSpec{
						Rules: []networkingv1.IngressRule{
							{
								Host:             "another.domain.net",
								IngressRuleValue: networkingv1.IngressRuleValue{},
							},
						},
					},
				},
			},
			DomainVerification: kuadrantv1.DomainVerification{
				ObjectMeta: v1.ObjectMeta{
					Name:      "dv",
					Namespace: "",
				},
				Spec: kuadrantv1.DomainVerificationSpec{
					Domain: "pb-custom.hcpapps.net",
				},
				Status: kuadrantv1.DomainVerificationStatus{
					Verified: true,
				},
			},
			ExpectedEnqueued: []string{"enqueued"},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			c := &Controller{
				lister: &mockLister{
					ingresses: scenario.Ingresses,
				},
				Controller: &reconciler.Controller{
					Logger: logr.Discard(),
				},
			}

			toEnqueue, err := c.ingressesFromDomainVerification(&scenario.DomainVerification)
			if err != nil {
				t.Fatal(err)
			}

			expected := sets.NewString(scenario.ExpectedEnqueued...)
			got := sets.NewString()
			for _, ingress := range toEnqueue {
				got.Insert(ingress.Name)
			}

			if !expected.Equal(got) {
				t.Errorf("unmatched ingresses to queue. Expected %v, got %v", expected, got)
			}
		})

	}
}
