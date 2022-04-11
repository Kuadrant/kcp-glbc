package main

import (
	"flag"
	"time"

	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	certmanclient "github.com/jetstack/cert-manager/pkg/client/clientset/versioned/typed/certmanager/v1"

	"github.com/kcp-dev/apimachinery/pkg/logicalcluster"

	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/clientset/versioned"
	"github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/informers/externalversions"
	"github.com/kuadrant/kcp-glbc/pkg/net"
	"github.com/kuadrant/kcp-glbc/pkg/reconciler/deployment"
	"github.com/kuadrant/kcp-glbc/pkg/reconciler/dns"
	"github.com/kuadrant/kcp-glbc/pkg/reconciler/ingress"
	"github.com/kuadrant/kcp-glbc/pkg/reconciler/service"
	tlsreconciler "github.com/kuadrant/kcp-glbc/pkg/reconciler/tls"
	"github.com/kuadrant/kcp-glbc/pkg/tls"
	"github.com/kuadrant/kcp-glbc/pkg/tls/certmanager"
	"github.com/kuadrant/kcp-glbc/pkg/util/os"
)

const (
	numThreads   = 2
	resyncPeriod = 10 * time.Hour
)

var kubeconfig = flag.String("kubeconfig", "", "Path to kubeconfig")
var logicalClusterTarget = flag.String("logical-cluster", os.GetEnvString("GLBC_LOGICAL_CLUSTER_TARGET", "*"), "set the target logical cluster")
var glbcKubeconfig = flag.String("glbc-kubeconfig", "", "Path to GLBC kubeconfig")
var tlsProviderEnabled = flag.Bool("glbc-tls-provided", os.GetEnvBool("GLBC_TLS_PROVIDED", false), "when set to true glbc will generate LE certs for hosts it creates")
var tlsProvider = flag.String("glbc-tls-provider", os.GetEnvString("GLBC_TLS_PROVIDER", "le-staging"), "decides which provider to use. Current allowed values -glbc-tls-provider=le-staging -glbc-tls-provider=le-production ")
var region = flag.String("region", os.GetEnvString("AWS_REGION", "eu-central-1"), "the region we should target with AWS clients")
var kubecontext = flag.String("context", os.GetEnvString("GLBC_KCP_CONTEXT", ""), "Context to use in the Kubeconfig file, instead of the current context")

var domain = flag.String("domain", os.GetEnvString("GLBC_DOMAIN", "hcpapps.net"), "The domain to use to expose ingresses")
var enableCustomHosts = flag.Bool("enable-custom-hosts", os.GetEnvBool("GLBC_ENABLE_CUSTOM_HOSTS", false), "Flag to enable hosts to be custom")

var dnsProvider = flag.String("dns-provider", os.GetEnvString("GLBC_DNS_PROVIDER", "aws"), "The DNS provider being used [aws, fake]")

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	var overrides clientcmd.ConfigOverrides
	if *kubecontext != "" {
		overrides.CurrentContext = *kubecontext
	}

	r, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: *kubeconfig},
		&overrides).ClientConfig()
	if err != nil {
		klog.Fatal(err)
	}

	gr, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: *glbcKubeconfig},
		&clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		klog.Fatal(err)
	}

	ctx := genericapiserver.SetupSignalContext()

	kubeClient, err := kubernetes.NewClusterForConfig(r)
	if err != nil {
		klog.Fatal(err)
	}
	kubeInformerFactory := informers.NewSharedInformerFactory(kubeClient.Cluster(logicalcluster.New(*logicalClusterTarget)), resyncPeriod)

	dnsRecordClient, err := kuadrantv1.NewClusterForConfig(r)
	if err != nil {
		klog.Fatal(err)
	}
	kuadrantInformerFactory := externalversions.NewSharedInformerFactory(dnsRecordClient.Cluster(logicalcluster.New(*logicalClusterTarget)), resyncPeriod)

	// glbcTypedClient targets the control cluster (this is the cluster where glbc is deployed). This is not a KCP cluster.
	glbcTypedClient, err := kubernetes.NewForConfig(gr)
	if err != nil {
		klog.Fatal(err)
	}
	tlsCertProvider := certmanager.CertProviderLEStaging
	if *tlsProvider == "le-production" {
		tlsCertProvider = certmanager.CertProviderLEProd
	}
	klog.Info("using tls cert provider ", tlsCertProvider, *tlsProvider)

	// certman client targets the control cluster, this is the same cluster as glbc is deployed to
	certClient := certmanclient.NewForConfigOrDie(gr)
	certConfig := certmanager.CertManagerConfig{
		DNSValidator: certmanager.DNSValidatorRoute53,
		CertClient:   certClient,
		CertProvider: tlsCertProvider,
		Region:       *region,
		K8sClient:    glbcTypedClient,
		ValidDomans:  []string{*domain},
	}
	var certProvider tls.Provider = &tls.FakeProvider{}
	if *tlsProviderEnabled {
		certProvider, err = certmanager.NewCertManager(certConfig)
		if err != nil {
			klog.Fatal(err)
		}
	}

	// ensure Issuer Is Setup at start up time
	// TODO consider extracting out the setup to CRD
	if err := certProvider.Initialize(ctx); err != nil {
		klog.Fatal(err)
	}
	glbcFilteredInformerFactory := informers.NewFilteredSharedInformerFactory(glbcTypedClient, time.Minute, "cert-manager", nil)
	tlsController, err := tlsreconciler.NewController(&tlsreconciler.ControllerConfig{
		SharedInformerFactory: glbcFilteredInformerFactory,
		GlbcKubeClient:        glbcTypedClient,
		KcpClient:             kubeClient,
	})
	if err != nil {
		klog.Fatal(err)
	}

	controllerConfig := &ingress.ControllerConfig{
		KubeClient:            kubeClient,
		DnsRecordClient:       dnsRecordClient,
		SharedInformerFactory: kubeInformerFactory,
		Domain:                domain,
		CertProvider:          certProvider,
		TLSEnabled:            *tlsProviderEnabled,
		HostResolver:          net.NewDefaultHostResolver(),
		// For testing. TODO: Make configurable through flags/env variable
		// HostResolver: &net.ConfigMapHostResolver{
		// 	Name:      "hosts",
		// 	Namespace: "default",
		// },
		CustomHostsEnabled: enableCustomHosts,
	}
	ingressController := ingress.NewController(controllerConfig)

	dnsRecordController, err := dns.NewController(&dns.ControllerConfig{
		DnsRecordClient:       dnsRecordClient,
		SharedInformerFactory: kuadrantInformerFactory,
		DNSProvider:           dnsProvider,
	})
	if err != nil {
		klog.Fatal(err)
	}

	serviceController, err := service.NewController(&service.ControllerConfig{
		ServicesClient:        kubeClient,
		SharedInformerFactory: kubeInformerFactory,
	})
	if err != nil {
		klog.Fatal(err)
	}

	deploymentController, err := deployment.NewController(&deployment.ControllerConfig{
		DeploymentClient:      kubeClient,
		SharedInformerFactory: kubeInformerFactory,
	})
	if err != nil {
		klog.Fatal(err)
	}

	kubeInformerFactory.Start(ctx.Done())
	kubeInformerFactory.WaitForCacheSync(ctx.Done())

	kuadrantInformerFactory.Start(ctx.Done())
	kuadrantInformerFactory.WaitForCacheSync(ctx.Done())

	glbcFilteredInformerFactory.Start(ctx.Done())
	glbcFilteredInformerFactory.WaitForCacheSync(ctx.Done())

	go func() {
		ingressController.Start(ctx, numThreads)
	}()

	go func() {
		dnsRecordController.Start(ctx, numThreads)
	}()

	go func() {
		tlsController.Start(ctx, numThreads)
	}()

	go func() {
		serviceController.Start(ctx, numThreads)
	}()

	go func() {
		deploymentController.Start(ctx, numThreads)
	}()

	<-ctx.Done()
}
