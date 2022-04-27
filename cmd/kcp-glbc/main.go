package main

import (
	"context"
	"flag"
	"sync"
	"time"

	// Make sure our workqueue MetricsProvider is the first to register
	_ "github.com/kuadrant/kcp-glbc/pkg/reconciler"

	"golang.org/x/sync/errgroup"

	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	certmanclient "github.com/jetstack/cert-manager/pkg/client/clientset/versioned/typed/certmanager/v1"

	"github.com/kcp-dev/apimachinery/pkg/logicalcluster"

	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/clientset/versioned"
	"github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/informers/externalversions"
	"github.com/kuadrant/kcp-glbc/pkg/metrics"
	"github.com/kuadrant/kcp-glbc/pkg/net"
	"github.com/kuadrant/kcp-glbc/pkg/reconciler/deployment"
	"github.com/kuadrant/kcp-glbc/pkg/reconciler/dns"
	"github.com/kuadrant/kcp-glbc/pkg/reconciler/ingress"
	"github.com/kuadrant/kcp-glbc/pkg/reconciler/service"
	tlsreconciler "github.com/kuadrant/kcp-glbc/pkg/reconciler/tls"
	"github.com/kuadrant/kcp-glbc/pkg/tls"
	"github.com/kuadrant/kcp-glbc/pkg/util/env"
)

const (
	numThreads   = 2
	resyncPeriod = 10 * time.Hour
)

var (
	// Control cluster client options
	glbcKubeconfig = flag.String("glbc-kubeconfig", "", "Path to GLBC kubeconfig")
	// KCP client options
	kubeconfig           = flag.String("kubeconfig", "", "Path to kubeconfig")
	kubecontext          = flag.String("context", env.GetEnvString("GLBC_KCP_CONTEXT", ""), "Context to use in the Kubeconfig file, instead of the current context")
	logicalClusterTarget = flag.String("logical-cluster", env.GetEnvString("GLBC_LOGICAL_CLUSTER_TARGET", "*"), "set the target logical cluster")
	// TLS certificate issuance options
	tlsProviderEnabled = flag.Bool("glbc-tls-provided", env.GetEnvBool("GLBC_TLS_PROVIDED", false), "when set to true glbc will generate LE certs for hosts it creates")
	tlsProvider        = flag.String("glbc-tls-provider", env.GetEnvString("GLBC_TLS_PROVIDER", "le-staging"), "decides which provider to use. Current allowed values -glbc-tls-provider=le-staging -glbc-tls-provider=le-production ")
	// DNS management options
	domain            = flag.String("domain", env.GetEnvString("GLBC_DOMAIN", "hcpapps.net"), "The domain to use to expose ingresses")
	enableCustomHosts = flag.Bool("enable-custom-hosts", env.GetEnvBool("GLBC_ENABLE_CUSTOM_HOSTS", false), "Flag to enable hosts to be custom")
	dnsProvider       = flag.String("dns-provider", env.GetEnvString("GLBC_DNS_PROVIDER", "aws"), "The DNS provider being used [aws, fake]")
	// AWS Route53 options
	region = flag.String("region", env.GetEnvString("AWS_REGION", "eu-central-1"), "the region we should target with AWS clients")
	// Observability options
	monitoringPort = flag.Int("monitoring-port", 8080, "The port of the metrics endpoint (can be set to \"0\" to disable the metrics serving)")
)

var controllersGroup = sync.WaitGroup{}

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
	tlsCertProvider := tls.CertProviderLEStaging
	if *tlsProvider == "le-production" {
		tlsCertProvider = tls.CertProviderLEProd
	}
	klog.Info("using tls cert provider ", tlsCertProvider, *tlsProvider)

	// certman client targets the control cluster, this is the same cluster as glbc is deployed to
	certClient := certmanclient.NewForConfigOrDie(gr)
	certConfig := tls.CertManagerConfig{
		DNSValidator: tls.DNSValidatorRoute53,
		CertClient:   certClient,
		CertProvider: tlsCertProvider,
		Region:       *region,
		K8sClient:    glbcTypedClient,
		ValidDomains: []string{*domain},
	}
	var certProvider tls.Provider = &tls.FakeProvider{}
	if *tlsProviderEnabled {
		certProvider, err = tls.NewCertManager(certConfig)
		if err != nil {
			klog.Fatal(err)
		}
	}
	tlsreconciler.InitMetrics(certProvider)

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

	// start listening on the metrics endpoint
	metricsServer, err := metrics.NewServer(monitoringPort)
	if err != nil {
		klog.Exitf("Failed to create metrics server: %v", err)
	}

	g, gCtx := errgroup.WithContext(ctx)

	g.Go(metricsServer.Start)

	start(gCtx, ingressController)
	start(gCtx, dnsRecordController)
	start(gCtx, tlsController)
	start(gCtx, serviceController)
	start(gCtx, deploymentController)

	g.Go(func() error {
		// wait until the controllers have return before stopping serving metrics
		controllersGroup.Wait()
		return metricsServer.Shutdown()
	})

	if err := g.Wait(); err != nil {
		klog.Exitf("Exiting due to error: %v", err)
	}
}

type Controller interface {
	Start(context.Context, int)
}

func start(ctx context.Context, runnable Controller) {
	controllersGroup.Add(1)
	go func() {
		defer controllersGroup.Done()
		runnable.Start(ctx, numThreads)
	}()
}
