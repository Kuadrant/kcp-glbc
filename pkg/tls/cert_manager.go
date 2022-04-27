/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tls

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	cmacme "github.com/jetstack/cert-manager/pkg/apis/acme/v1"
	certman "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/jetstack/cert-manager/pkg/apis/meta/v1"
	certmanclient "github.com/jetstack/cert-manager/pkg/client/clientset/versioned/typed/certmanager/v1"

	v1 "k8s.io/api/core/v1"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

type DNSValidator int

const (
	DNSValidatorRoute53 DNSValidator = iota
)

type CertProvider string

const (
	awsSecretName         string       = "route53-credentials"
	envAwsAccessKeyID     string       = "AWS_ACCESS_KEY_ID"
	envAwsAccessSecret    string       = "AWS_SECRET_ACCESS_KEY"
	envAwsZoneID          string       = "AWS_DNS_PUBLIC_ZONE_ID"
	envLEEmail            string       = "HCG_LE_EMAIL"
	CertProviderLEStaging CertProvider = "letsencryptstaging"
	CertProviderLEProd    CertProvider = "letsencryptprod"
	leProdAPI             string       = "https://acme-v02.api.letsencrypt.org/directory"
	leStagingAPI          string       = "https://acme-staging-v02.api.letsencrypt.org/directory"
	defaultCertificateNS  string       = "cert-manager"
)

// CertManager is a certificate provider.
type CertManager struct {
	dnsValidationProvider DNSValidator
	certClient            certmanclient.CertmanagerV1Interface
	k8sClient             kubernetes.Interface
	certProvider          CertProvider
	LEConfig              LEConfig
	Region                string
	certificateNS         string
	validDomains          []string
}

var _ Provider = &CertManager{}

type LEConfig struct {
	Email string
}

type CertManagerConfig struct {
	DNSValidator DNSValidator
	CertClient   certmanclient.CertmanagerV1Interface

	CertProvider CertProvider
	LEConfig     *LEConfig
	Region       string
	// client targeting the control cluster
	K8sClient kubernetes.Interface
	// namespace in the control cluster where we create certificates
	CertificateNS string
	// set of domains we allow certs to be created for
	ValidDomains []string
}

func awsSecret() v1.Secret {
	accessKeyID := os.Getenv(envAwsAccessKeyID)
	accessSecret := os.Getenv(envAwsAccessSecret)
	zoneID := os.Getenv(envAwsZoneID)

	data := make(map[string][]byte)

	data[envAwsAccessKeyID] = []byte(accessKeyID)
	data[envAwsAccessSecret] = []byte(accessSecret)
	data[envAwsZoneID] = []byte(zoneID)
	return v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: awsSecretName,
		},
		Data: data,
	}
}

func NewCertManager(c CertManagerConfig) (*CertManager, error) {
	cm := &CertManager{
		dnsValidationProvider: c.DNSValidator,
		certClient:            c.CertClient,
		k8sClient:             c.K8sClient,
		certProvider:          c.CertProvider,
		Region:                c.Region,
		validDomains:          c.ValidDomains,
	}

	if c.LEConfig == nil {
		cm.LEConfig = LEConfig{}
	}

	if os.Getenv(envAwsAccessKeyID) == "" || os.Getenv(envAwsAccessSecret) == "" || os.Getenv(envAwsZoneID) == "" {
		return nil, fmt.Errorf(fmt.Sprintf("certmanager is missing envars for aws %s %s %s", envAwsAccessKeyID, envAwsAccessSecret, envAwsZoneID))
	}
	if os.Getenv(envLEEmail) == "" {
		return nil, fmt.Errorf("certmanager: missing env var %s", envLEEmail)
	}

	if cm.certificateNS == "" {
		cm.certificateNS = defaultCertificateNS
	}
	return cm, nil
}

func (cm *CertManager) IssuerID() string {
	return string(cm.certProvider)
}

func (cm *CertManager) Domains() []string {
	return cm.validDomains
}

func (cm *CertManager) issuer(ns string) *certman.Issuer {
	ci := certman.Issuer{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      string(cm.certProvider),
		},
		Spec: certman.IssuerSpec{
			IssuerConfig: certman.IssuerConfig{
				ACME: &cmacme.ACMEIssuer{
					Email:  os.Getenv(envLEEmail),
					Server: leStagingAPI,
					PrivateKey: cmmeta.SecretKeySelector{
						LocalObjectReference: cmmeta.LocalObjectReference{
							Name: string(cm.certProvider),
						},
					},
					Solvers: []cmacme.ACMEChallengeSolver{
						{
							DNS01: &cmacme.ACMEChallengeSolverDNS01{
								Route53: &cmacme.ACMEIssuerDNS01ProviderRoute53{
									AccessKeyID:  os.Getenv(envAwsAccessKeyID),
									HostedZoneID: os.Getenv(envAwsZoneID),
									Region:       cm.Region,
									SecretAccessKey: cmmeta.SecretKeySelector{
										LocalObjectReference: cmmeta.LocalObjectReference{
											Name: awsSecretName,
										},
										Key: envAwsAccessSecret,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if cm.certProvider == CertProviderLEProd {
		ci.Spec.ACME.Server = leProdAPI
	}
	return &ci
}

// Initialize will configure the issuer and aws access secret.
// TODO this should probably be in a reconciler triggered by a GLBC TLS CRD (so that it can set up multiple issuers and reconcile any changes. )
func (cm *CertManager) Initialize(ctx context.Context) error {
	// TODO we might want to create CRD to watch for setup
	ns := cm.certificateNS
	secretClient := cm.k8sClient.CoreV1().Secrets(ns)
	klog.InfoS("using cert provider ", "provider", cm.certProvider)
	// create secret with AWS values
	awsSecret := awsSecret()
	_, err := secretClient.Create(ctx, &awsSecret, metav1.CreateOptions{})
	if err != nil && !k8errors.IsAlreadyExists(err) {
		return err
	}
	if err != nil {
		s, err := secretClient.Get(ctx, awsSecret.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		s.Data = awsSecret.Data
		_, err = secretClient.Update(ctx, s, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	issuer := cm.issuer(ns)
	_, err = cm.certClient.Issuers(ns).Create(ctx, issuer, metav1.CreateOptions{})
	if err != nil && !k8errors.IsAlreadyExists(err) {
		return err
	}
	if err != nil {
		is, err := cm.certClient.Issuers(ns).Get(ctx, issuer.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		issuer.ResourceVersion = is.ResourceVersion
		if _, err := cm.certClient.Issuers(ns).Update(ctx, issuer, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}

	return nil
}

func (cm *CertManager) certificate(cr CertificateRequest) *certman.Certificate {
	annotations := cr.Annotations()
	annotations[tlsIssuerAnnotation] = cm.IssuerID()
	return &certman.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name(),
			Namespace: cm.certificateNS,
		},
		Spec: certman.CertificateSpec{
			SecretName: cr.Name(),
			SecretTemplate: &certman.CertificateSecretTemplate{
				Labels:      cr.Labels(),
				Annotations: annotations,
			},
			// TODO Some of the below should be pulled out into a CRD
			Duration: &metav1.Duration{
				Duration: time.Hour * 24 * 90, // cert lasts for 90 days
			},
			RenewBefore: &metav1.Duration{
				Duration: time.Hour * 24 * 15, // cert is renewed 15 days before hand
			},
			PrivateKey: &certman.CertificatePrivateKey{
				Algorithm: certman.RSAKeyAlgorithm,
				Encoding:  certman.PKCS1,
				Size:      2048,
			},
			Usages:   certman.DefaultKeyUsages(),
			DNSNames: []string{cr.Host()},
			IssuerRef: cmmeta.ObjectReference{
				Group: "cert-manager.io",
				Kind:  "Issuer",
				Name:  string(cm.certProvider),
			},
		},
	}
}

func isValidDomain(host string, allowed []string) bool {
	for _, v := range allowed {
		if strings.HasSuffix(host, v) {
			return true
		}
	}
	return false
}

func (cm *CertManager) Create(ctx context.Context, cr CertificateRequest) error {
	if !isValidDomain(cr.Host(), cm.validDomains) {
		return fmt.Errorf("cannot create certificate for host %s invalid domain", cr.Host())
	}
	cert := cm.certificate(cr)
	_, err := cm.certClient.Certificates(cm.certificateNS).Create(ctx, cert, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	// TODO: Move to Certificate informer add handler
	CertificateRequestCount.WithLabelValues(cm.IssuerID(), cr.Host()).Inc()
	return nil
}

func (cm *CertManager) Delete(ctx context.Context, cr CertificateRequest) error {
	// delete the certificate and delete the secrets
	certNotFound := false
	if err := cm.certClient.Certificates(cm.certificateNS).Delete(ctx, cr.Name(), metav1.DeleteOptions{}); err != nil {
		if !k8errors.IsNotFound(err) {
			return err
		}
		certNotFound = true
	}
	if err := cm.k8sClient.CoreV1().Secrets(cm.certificateNS).Delete(ctx, cr.Name(), metav1.DeleteOptions{}); err != nil {
		if !k8errors.IsNotFound(err) {
			return err
		}
		if !certNotFound {
			// The Secret does not exist, which indicates the TLS certificate request is still pending,
			// so we must account for decreasing the number of pending requests.
			// TODO: Move to Certificate informer delete handler
			CertificateRequestCount.WithLabelValues(cm.IssuerID(), cr.Host()).Dec()
		}
	}
	return nil
}
