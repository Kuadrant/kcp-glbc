//go:build e2e
// +build e2e

package support

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	kcp "github.com/kcp-dev/kcp/pkg/client/clientset/versioned"

	kuadrantv1 "github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/clientset/versioned"
)

type Client interface {
	Core() kubernetes.ClusterInterface
	Kcp() kcp.ClusterInterface
	Kuadrant() kuadrantv1.ClusterInterface
	GetConfig() *rest.Config
}

type client struct {
	core     kubernetes.ClusterInterface
	kcp      kcp.ClusterInterface
	kuadrant kuadrantv1.ClusterInterface
	config   *rest.Config
}

func (c *client) Core() kubernetes.ClusterInterface {
	return c.core
}

func (c *client) Kcp() kcp.ClusterInterface {
	return c.kcp
}

func (c *client) Kuadrant() kuadrantv1.ClusterInterface {
	return c.kuadrant
}

func (c *client) GetConfig() *rest.Config {
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

	kuandrantClient, err := kuadrantv1.NewClusterForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &client{
		core:     kubeClient,
		kcp:      kcpClient,
		kuadrant: kuandrantClient,
		config:   cfg,
	}, nil
}
