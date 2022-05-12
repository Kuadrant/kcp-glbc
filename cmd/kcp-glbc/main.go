package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"

	// Make sure our workqueue MetricsProvider is the first to register
	_ "github.com/kuadrant/kcp-glbc/pkg/reconciler"

	certmanclient "github.com/jetstack/cert-manager/pkg/client/clientset/versioned/typed/certmanager/v1"

	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/kcp-dev/apimachinery/pkg/logicalcluster"

	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/clientset/versioned"
	"github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/informers/externalversions"
	"github.com/kuadrant/kcp-glbc/pkg/log"
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

var options struct {
	// The path to the GLBC kubeconfig
	GlbcKubeconfig string
	// The path to the KCP kubeconfig
	Kubeconfig string
	// The KCP context
	Kubecontext string
	// The KCP logical cluster
	LogicalClusterTarget string
	// Whether to generate TLS certificates for hosts
	TLSProviderEnabled bool
	// The TLS certificate issuer
	TLSProvider string
	// The base domain
	Domain string
	// Whether custom hosts are permitted
	EnableCustomHosts bool
	// The DNS provider
	DNSProvider string
	// The AWS Route53 region
	Region string
	// The port number of the metrics endpoint
	MonitoringPort int
}

func init() {
	flagSet := flag.CommandLine

	// Control cluster client options
	flagSet.StringVar(&options.GlbcKubeconfig, "glbc-kubeconfig", "", "Path to GLBC kubeconfig")
	// KCP client options
	flagSet.StringVar(&options.Kubeconfig, "kubeconfig", "", "Path to kubeconfig")
	flagSet.StringVar(&options.Kubecontext, "context", env.GetEnvString("GLBC_KCP_CONTEXT", ""), "Context to use in the Kubeconfig file, instead of the current context")
	flagSet.StringVar(&options.LogicalClusterTarget, "logical-cluster", env.GetEnvString("GLBC_LOGICAL_CLUSTER_TARGET", "*"), "set the target logical cluster")
	// TLS certificate issuance options
	flagSet.BoolVar(&options.TLSProviderEnabled, "glbc-tls-provided", env.GetEnvBool("GLBC_TLS_PROVIDED", false), "Whether to generate TLS certificates for hosts")
	flagSet.StringVar(&options.TLSProvider, "glbc-tls-provider", env.GetEnvString("GLBC_TLS_PROVIDER", "glbc-ca"), "The TLS certificate issuer, one of [glbc-ca, le-staging, le-production]")
	// DNS management options
	flagSet.StringVar(&options.Domain, "domain", env.GetEnvString("GLBC_DOMAIN", "hcpapps.net"), "The domain to use to expose ingresses")
	flagSet.BoolVar(&options.EnableCustomHosts, "enable-custom-hosts", env.GetEnvBool("GLBC_ENABLE_CUSTOM_HOSTS", false), "Flag to enable hosts to be custom")
	flag.StringVar(&options.DNSProvider, "dns-provider", env.GetEnvString("GLBC_DNS_PROVIDER", "aws"), "The DNS provider being used [aws, fake]")
	// // AWS Route53 options
	flag.StringVar(&options.Region, "region", env.GetEnvString("AWS_REGION", "eu-central-1"), "the region we should target with AWS clients")
	//  Observability options
	flagSet.IntVar(&options.MonitoringPort, "monitoring-port", 8080, "The port of the metrics endpoint (can be set to \"0\" to disable the metrics serving)")

	opts := log.Options{
		EncoderConfigOptions: []log.EncoderConfigOption{
			func(c *zapcore.EncoderConfig) {
				c.ConsoleSeparator = " "
			},
		},
		ZapOpts: []zap.Option{
			zap.AddCaller(),
		},
	}
	opts.BindFlags(flag.CommandLine)
	klog.InitFlags(flag.CommandLine)
	flag.Parse()

	log.Logger = log.New(log.UseFlagOptions(&opts))
	klog.SetLogger(log.Logger)
}

var controllersGroup = sync.WaitGroup{}

func main() {
	var overrides clientcmd.ConfigOverrides
	if options.Kubecontext != "" {
		overrides.CurrentContext = options.Kubecontext
	}

	kcpClientConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: options.Kubeconfig},
		&overrides).ClientConfig()
	exitOnError(err, "Failed to create KCP config")

	glbcClientConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: options.GlbcKubeconfig},
		&clientcmd.ConfigOverrides{}).ClientConfig()
	exitOnError(err, "Failed to create K8S config")

	ctx := genericapiserver.SetupSignalContext()

	kcpKubeClient, err := kubernetes.NewClusterForConfig(kcpClientConfig)
	exitOnError(err, "Failed to create KCP core client")
	kcpKubeInformerFactory := informers.NewSharedInformerFactory(kcpKubeClient.Cluster(logicalcluster.New(options.LogicalClusterTarget)), resyncPeriod)

	kcpKuadrantClient, err := kuadrantv1.NewClusterForConfig(kcpClientConfig)
	exitOnError(err, "Failed to create KCP kuadrant client")
	kcpKuadrantInformerFactory := externalversions.NewSharedInformerFactory(kcpKuadrantClient.Cluster(logicalcluster.New(options.LogicalClusterTarget)), resyncPeriod)

	// glbcKubeClient targets the control cluster (this is the cluster where glbc is deployed).
	// This is not a KCP cluster.
	glbcKubeClient, err := kubernetes.NewForConfig(glbcClientConfig)
	exitOnError(err, "Failed to create K8S core client")

	namespace := env.GetNamespace()

	var certProvider tls.Provider
	if options.TLSProviderEnabled {
		if namespace == "" {
			namespace = tls.DefaultCertificateNS
		}

		var tlsCertProvider tls.CertProvider
		switch options.TLSProvider {
		case "glbc-ca":
			tlsCertProvider = tls.CertProviderCA
		case "le-staging":
			tlsCertProvider = tls.CertProviderLEStaging
		case "le-production":
			tlsCertProvider = tls.CertProviderLEProd
		default:
			exitOnError(fmt.Errorf("unsupported TLS certificate issuer: %s", options.TLSProvider), "Failed to create cert provider")
		}

		log.Logger.Info("Creating TLS certificate provider", "issuer", tlsCertProvider)

		certProvider, err = tls.NewCertManager(tls.CertManagerConfig{
			DNSValidator:  tls.DNSValidatorRoute53,
			CertClient:    certmanclient.NewForConfigOrDie(glbcClientConfig),
			CertProvider:  tlsCertProvider,
			Region:        options.Region,
			K8sClient:     glbcKubeClient,
			ValidDomains:  []string{options.Domain},
			CertificateNS: namespace,
		})
		exitOnError(err, "Failed to create cert provider")

		tlsreconciler.InitMetrics(certProvider)

		// ensure Issuer is setup at start up time
		// TODO consider extracting out the setup to CRD
		err = certProvider.Initialize(ctx)
		exitOnError(err, "Failed to initialize cert provider")
	}

	glbcKubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(glbcKubeClient, time.Minute, informers.WithNamespace(namespace))
	tlsController, err := tlsreconciler.NewController(&tlsreconciler.ControllerConfig{
		GlbcSecretInformer: glbcKubeInformerFactory.Core().V1().Secrets(),
		GlbcKubeClient:     glbcKubeClient,
		KcpKubeClient:      kcpKubeClient,
	})
	exitOnError(err, "Failed to create TLS certificate controller")

	ingressController := ingress.NewController(&ingress.ControllerConfig{
		KubeClient:            kcpKubeClient,
		DnsRecordClient:       kcpKuadrantClient,
		SharedInformerFactory: kcpKubeInformerFactory,
		Domain:                options.Domain,
		CertProvider:          certProvider,
		HostResolver:          net.NewDefaultHostResolver(),
		// For testing. TODO: Make configurable through flags/env variable
		// HostResolver: &net.ConfigMapHostResolver{
		// 	Name:      "hosts",
		// 	Namespace: "default",
		// },
		CustomHostsEnabled: options.EnableCustomHosts,
	})

	dnsRecordController, err := dns.NewController(&dns.ControllerConfig{
		DnsRecordClient:       kcpKuadrantClient,
		SharedInformerFactory: kcpKuadrantInformerFactory,
		DNSProvider:           options.DNSProvider,
	})
	exitOnError(err, "Failed to create DNSRecord controller")

	serviceController, err := service.NewController(&service.ControllerConfig{
		ServicesClient:        kcpKubeClient,
		SharedInformerFactory: kcpKubeInformerFactory,
	})
	exitOnError(err, "Failed to create Service controller")

	deploymentController, err := deployment.NewController(&deployment.ControllerConfig{
		DeploymentClient:      kcpKubeClient,
		SharedInformerFactory: kcpKubeInformerFactory,
	})
	exitOnError(err, "Failed to create Deployment controller")

	kcpKubeInformerFactory.Start(ctx.Done())
	kcpKubeInformerFactory.WaitForCacheSync(ctx.Done())

	kcpKuadrantInformerFactory.Start(ctx.Done())
	kcpKuadrantInformerFactory.WaitForCacheSync(ctx.Done())

	if options.TLSProviderEnabled {
		// the control cluster Kube informer is only used when TLS certificate issuance is enabled
		glbcKubeInformerFactory.Start(ctx.Done())
		glbcKubeInformerFactory.WaitForCacheSync(ctx.Done())
	}

	// start listening on the metrics endpoint
	metricsServer, err := metrics.NewServer(options.MonitoringPort)
	exitOnError(err, "Failed to create metrics server")

	g, gCtx := errgroup.WithContext(ctx)

	g.Go(metricsServer.Start)

	start(gCtx, ingressController)
	start(gCtx, dnsRecordController)
	if options.TLSProviderEnabled {
		start(gCtx, tlsController)
	}
	start(gCtx, serviceController)
	start(gCtx, deploymentController)

	g.Go(func() error {
		// wait until the controllers have return before stopping serving metrics
		controllersGroup.Wait()
		return metricsServer.Shutdown()
	})

	exitOnError(g.Wait(), "Exiting due to error")
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

func exitOnError(err error, msg string) {
	if err != nil {
		log.Logger.Error(err, msg)
		os.Exit(1)
	}
}
