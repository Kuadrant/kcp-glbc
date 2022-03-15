package e2e

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	kcp "github.com/kcp-dev/kcp/pkg/client/clientset/versioned"
)

type Client interface {
	Core() kubernetes.ClusterInterface
	Kcp() kcp.ClusterInterface
	GetConfig() *rest.Config
}

type defaultClient struct {
	core   kubernetes.ClusterInterface
	kcp    kcp.ClusterInterface
	config *rest.Config
}

func (c *defaultClient) Core() kubernetes.ClusterInterface {
	return c.core
}

func (c *defaultClient) Kcp() kcp.ClusterInterface {
	return c.kcp
}

func (c *defaultClient) GetConfig() *rest.Config {
	return c.config
}

func newTestClient() (Client, error) {
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{
			CurrentContext: "admin",
		}).ClientConfig()
	if err != nil {
		return nil, err
	}

	kubeClient, err := kubernetes.NewClusterForConfig(cfg)
	if err != nil {
		return nil, err
	}

	kcpClient, err := kcp.NewClusterForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &defaultClient{
		core:   kubeClient,
		kcp:    kcpClient,
		config: cfg,
	}, nil
}
