package ingress

import (
	"encoding/json"
	"testing"

	"github.com/go-logr/logr"
	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	"github.com/kuadrant/kcp-glbc/pkg/cluster"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_awsEndpointWeight(t *testing.T) {
	type args struct {
		numIPs int
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "single ip",
			args: args{
				numIPs: 1,
			},
			want: "120",
		},
		{
			name: "multiple ips 2",
			args: args{
				numIPs: 2,
			},
			want: "60",
		},
		{
			name: "multiple ips 3",
			args: args{
				numIPs: 3,
			},
			want: "40",
		},
		{
			name: "multiple ips 4",
			args: args{
				numIPs: 4,
			},
			want: "30",
		},
		{
			name: "60 ips",
			args: args{
				numIPs: 60,
			},
			want: "2",
		},
		{
			name: "61 ips",
			args: args{
				numIPs: 61,
			},
			want: "1",
		},
		{
			name: "ips equal to max weight (120)",
			args: args{
				numIPs: 120,
			},
			want: "1",
		},
		{
			name: "more IPs than max weight (121)",
			args: args{
				numIPs: 121,
			},
			want: "1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := awsEndpointWeight(tt.args.numIPs); got != tt.want {
				t.Errorf("awsEndpointWeight() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProcessCustomHostValidation(t *testing.T) {
	testCases := []struct {
		name                   string
		ingress                *networkingv1.Ingress
		domainVerifications    *v1.DomainVerificationList
		expectedGeneratedRules map[string]int
		expectedRules          []networkingv1.IngressRule
	}{
		{
			name: "Empty host",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ingress",
					Annotations: map[string]string{
						cluster.ANNOTATION_HCG_HOST: "generated.host.net",
					},
				},
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{
							Host: "",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path: "/",
										},
									},
								},
							},
						},
					},
				},
			},
			domainVerifications: &v1.DomainVerificationList{},
			expectedGeneratedRules: map[string]int{
				"": 0,
			},
			expectedRules: []networkingv1.IngressRule{
				{
					Host: "generated.host.net",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path: "/",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Custom host verified",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ingress",
					Annotations: map[string]string{
						cluster.ANNOTATION_HCG_HOST: "generated.host.net",
					},
				},
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{
							Host: "test.pb-custom.hcpapps.net",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path: "/path",
										},
									},
								},
							},
						},
					},
				},
			},
			domainVerifications: &v1.DomainVerificationList{
				Items: []v1.DomainVerification{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pb-custom.hcpapps.net",
						},
						Spec: v1.DomainVerificationSpec{
							Domain: "pb-custom.hcpapps.net",
						},
						Status: v1.DomainVerificationStatus{
							Verified: true,
						},
					},
				},
			},
			expectedGeneratedRules: map[string]int{
				"test.pb-custom.hcpapps.net": 1,
			},
			expectedRules: []networkingv1.IngressRule{
				{
					Host: "test.pb-custom.hcpapps.net",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path: "/path",
								},
							},
						},
					},
				},
				{
					Host: "generated.host.net",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path: "/path",
								},
							},
						},
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ingress := testCase.ingress.DeepCopy()

			if err := doProcessCustomHostValidation(
				logr.Discard(),
				testCase.domainVerifications,
				ingress,
			); err != nil {
				t.Fatal(err)
			}

			// Assert the expected generated rules matches the
			// annotation
			if testCase.expectedGeneratedRules != nil {
				annotation, ok := ingress.Annotations[GeneratedRulesAnnotation]
				if !ok {
					t.Fatalf("expected GeneratedRulesAnnotation to be set")
				}

				generatedRules := map[string]int{}
				if err := json.Unmarshal(
					[]byte(annotation),
					&generatedRules,
				); err != nil {
					t.Fatalf("invalid format on GeneratedRulesAnnotation: %v", err)
				}

				for domain, index := range testCase.expectedGeneratedRules {
					if generatedRules[domain] != index {
						t.Errorf("expected generated rule for domain %s to be in index %d, but got %d",
							domain,
							index,
							generatedRules[domain],
						)
					}
				}
			}

			// Assert the reconciled rules match the expected rules
			for i, expectedRule := range testCase.expectedRules {
				rule := ingress.Spec.Rules[i]

				if !equality.Semantic.DeepEqual(expectedRule, rule) {
					t.Errorf("expected rule does not match rule at index %d", i)
				}
			}
		})
	}
}
