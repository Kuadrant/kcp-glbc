package main

import (
	"flag"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

	envoyserver "knative.dev/net-kourier/pkg/envoy/server"

	"github.com/kuadrant/kcp-ingress/pkg/reconciler/dns"
	"github.com/kuadrant/kcp-ingress/pkg/reconciler/ingress"
)

const numThreads = 2

var kubeconfig = flag.String("kubeconfig", "", "Path to kubeconfig")
var glbcKubeconfig = flag.String("glbc-kubeconfig", "", "Path to GLBC kubeconfig")
var kubecontext = flag.String("context", "", "Context to use in the Kubeconfig file, instead of the current context")

var domain = flag.String("domain", "hcpapps.net", "The domain to use to expose ingresses")
var dnsProvider = flag.String("dns-provider", "aws", "The DNS provider being used [aws, fake]")

var envoyEnableXDS = flag.Bool("envoyxds", false, "Start an Envoy control plane")
var envoyXDSPort = flag.Uint("envoyxds-port", 18000, "Envoy control plane port")
var envoyListenPort = flag.Uint("envoy-listener-port", 80, "Envoy default listener port")

func main() {
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

	controllerConfig := &ingress.ControllerConfig{
		Cfg:     r,
		GLBCCfg: gr,
		Domain:  domain,
	}

	if *envoyEnableXDS {
		controllerConfig.EnvoyXDS = envoyserver.NewXdsServer(*envoyXDSPort, nil)
		controllerConfig.EnvoyListenPort = envoyListenPort
	}

	go func() {
		ingress.NewController(controllerConfig).Start(numThreads)
	}()
	c, err := dns.NewController(&dns.ControllerConfig{Cfg: r, DNSProvider: dnsProvider})
	if err != nil {
		klog.Fatal(err)
	}
	c.Start(numThreads)
}
