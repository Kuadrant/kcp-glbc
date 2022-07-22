package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
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
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/kcp-dev/logicalcluster"

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
	// The path to the kcp kubeconfig
	Kubeconfig string
	// The kcp context
	Kubecontext string
	// The user compute workspace
	ComputeWorkspace string
	// The GLBC workspace
	GLBCWorkspace string
	// The kcp logical cluster
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

	// KCP client options
	flagSet.StringVar(&options.Kubeconfig, "kubeconfig", "", "Path to kubeconfig")
	flagSet.StringVar(&options.Kubecontext, "context", env.GetEnvString("GLBC_KCP_CONTEXT", ""), "Context to use in the Kubeconfig file, instead of the current context")
	flagSet.StringVar(&options.ComputeWorkspace, "compute-workspace", env.GetEnvString("GLBC_COMPUTE_WORKSPACE", "root:default:kcp-glbc-user-compute"), "The user compute workspace")
	flagSet.StringVar(&options.GLBCWorkspace, "glbc-workspace", env.GetEnvString("GLBC_WORKSPACE", "root:default:kcp-glbc"), "The GLBC workspace")
	flagSet.StringVar(&options.LogicalClusterTarget, "logical-cluster", env.GetEnvString("GLBC_LOGICAL_CLUSTER_TARGET", "*"), "set the target logical cluster")
	// TLS certificate issuance options
	flagSet.BoolVar(&options.TLSProviderEnabled, "glbc-tls-provided", env.GetEnvBool("GLBC_TLS_PROVIDED", true), "Whether to generate TLS certificates for hosts")
	flagSet.StringVar(&options.TLSProvider, "glbc-tls-provider", env.GetEnvString("GLBC_TLS_PROVIDER", "glbc-ca"), "The TLS certificate issuer, one of [glbc-ca, le-staging, le-production]")
	// DNS management options
	flagSet.StringVar(&options.Domain, "domain", env.GetEnvString("GLBC_DOMAIN", "dev.hcpapps.net"), "The domain to use to expose ingresses")
	flagSet.BoolVar(&options.EnableCustomHosts, "enable-custom-hosts", env.GetEnvBool("GLBC_ENABLE_CUSTOM_HOSTS", false), "Flag to enable hosts to be custom")
	flag.StringVar(&options.DNSProvider, "dns-provider", env.GetEnvString("GLBC_DNS_PROVIDER", "fake"), "The DNS provider being used [aws, fake]")
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
	// start listening on the metrics endpoint
	metricsServer, err := metrics.NewServer(options.MonitoringPort)
	exitOnError(err, "Failed to create metrics server")

	ctx := genericapiserver.SetupSignalContext()
	g, gCtx := errgroup.WithContext(ctx)

	g.Go(metricsServer.Start)

	defaultClientConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{}).ClientConfig()
	exitOnError(err, "Failed to create K8S config")
	// defaultKubeClient the client to the GLBC workspace, that uses the kcp TMC feature that rewrites the service account so the in-cluster client connects back to kcp
	defaultKubeClient, err := kubernetes.NewForConfig(defaultClientConfig)
	exitOnError(err, "Failed to create K8S core client")

	// kcp bootstrap client
	kcpClientConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: options.Kubeconfig},
		&clientcmd.ConfigOverrides{CurrentContext: options.Kubecontext}).ClientConfig()
	exitOnError(err, "Failed to create KCP config")

	// kcp compute client, providing access to the APIs negotiated with workload clusters,
	// i.e., Ingress, Service, Deployment, bootstrapped from the kubernetes APIExport of the
	// GLBC compute workspace.
	computeClientConfig := rest.CopyConfig(kcpClientConfig)
	computeClientConfig.Host = getAPIExportVirtualWorkspaceURL(kcpClientConfig, "kubernetes", options.ComputeWorkspace)
	log.Logger.Info(fmt.Sprintf("computeClientConfig.Host: %s", computeClientConfig.Host))
	// kcpKubeClient the client configured with the compute APIExport virtual workspace URL, that consumes APIs that are provided by the compute (Service, Deployment, Ingress)
	kcpKubeClient, err := kubernetes.NewClusterForConfig(computeClientConfig)
	exitOnError(err, "Failed to create KCP core client")
	kcpKubeInformerFactory := informers.NewSharedInformerFactory(kcpKubeClient.Cluster(logicalcluster.New(options.LogicalClusterTarget)), resyncPeriod)

	// Override the Kubernetes client as create and delete operations are not working yet
	// via the APIExport virtual workspace API server.
	// See https://github.com/kcp-dev/kcp/issues/1253 for more details.
	kcpKubeClient, err = kubernetes.NewClusterForConfig(kcpClientConfig)
	exitOnError(err, "Failed to create KCP kuadrant client")

	// GLBC APIs client, i.e., for DNSRecord resources, bootstrapped from the GLBC workspace.
	glbcClientConfig := rest.CopyConfig(kcpClientConfig)
	glbcClientConfig.Host = getAPIExportVirtualWorkspaceURL(kcpClientConfig, "glbc", options.GLBCWorkspace)
	log.Logger.Info(fmt.Sprintf("glbcClientConfig.Host: %s", glbcClientConfig.Host))
	// kcpKuadrantClient the client configured with the GLBC APIExport virtual workspace URL, that consumes the DNSRecord API
	kcpKuadrantClient, err := kuadrantv1.NewClusterForConfig(glbcClientConfig)
	exitOnError(err, "Failed to create KCP kuadrant client")
	kcpKuadrantInformerFactory := externalversions.NewSharedInformerFactory(kcpKuadrantClient.Cluster(logicalcluster.New(options.LogicalClusterTarget)), resyncPeriod)

	// Override the Kuadrant client as create and delete operations are not working yet
	// via the APIExport virtual workspace API server.
	// See https://github.com/kcp-dev/kcp/issues/1253 for more details.
	kcpKuadrantClient, err = kuadrantv1.NewClusterForConfig(kcpClientConfig)
	exitOnError(err, "Failed to create KCP kuadrant client")

	namespace := env.GetNamespace()

	var certProvider tls.Provider
	if options.TLSProviderEnabled {
		if namespace == "" {
			namespace = tls.DefaultCertificateNS
		}

		// TLSProvider is mandatory when TLS is enabled
		if options.TLSProvider == "" {
			exitOnError(fmt.Errorf("TLS Provider not specified"), "Failed to create cert provider")
		}

		var tlsCertProvider tls.CertProvider = tls.CertProvider(options.TLSProvider)

		log.Logger.Info("Instantiating TLS certificate provider", "issuer", tlsCertProvider)

		certProvider, err = tls.NewCertManager(tls.CertManagerConfig{
			DNSValidator:  tls.DNSValidatorRoute53,
			CertClient:    certmanclient.NewForConfigOrDie(defaultClientConfig),
			CertProvider:  tlsCertProvider,
			Region:        options.Region,
			K8sClient:     defaultKubeClient,
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

	glbcKubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(defaultKubeClient, time.Minute, informers.WithNamespace(namespace))
	tlsController, err := tlsreconciler.NewController(&tlsreconciler.ControllerConfig{
		GlbcSecretInformer: glbcKubeInformerFactory.Core().V1().Secrets(),
		GlbcKubeClient:     defaultKubeClient,
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

func getAPIExportVirtualWorkspaceURL(config *rest.Config, exportName, exportPath string) string {
	cfg := rest.CopyConfig(config)
	u, err := url.Parse(cfg.Host)
	exitOnError(err, "Failed to parse client config host")
	u.Path = fmt.Sprintf("/services/apiexport/%s/%s", exportPath, exportName)
	return u.String()
}
