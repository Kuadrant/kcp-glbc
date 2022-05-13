//go:build e2e
// +build e2e

package support

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kcp-dev/apimachinery/pkg/logicalcluster"
	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
	workloadv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/workload/v1alpha1"
	workloadplugin "github.com/kcp-dev/kcp/pkg/cliplugins/workload/plugin"
)

var (
	GLBCResources = []string{"ingresses.networking.k8s.io", "services", "deployments.apps", "secrets", "configmaps"}

	WithKubeConfigByName = &withKubeConfigByName{}

	_ Option = &withKubeConfigByName{}
	_ Option = &withKubeConfigByID{}
	_ Option = &syncer{}
)

type withKubeConfigByName struct{}

func WithKubeConfigByID(id string) Option {
	return &withKubeConfigByID{id}
}

type withKubeConfigByID struct {
	ID string
}

func (o *withKubeConfigByName) applyTo(to interface{}) error {
	config, ok := to.(*workloadClusterConfig)
	if !ok {
		return fmt.Errorf("cannot apply WithKubeConfigByName to %q", to)
	}
	return WithKubeConfigByID(config.name).applyTo(config)
}

func (o *withKubeConfigByID) applyTo(to interface{}) error {
	config, ok := to.(*workloadClusterConfig)
	if !ok {
		return fmt.Errorf("cannot apply WithKubeConfigById to %q", to)
	}
	dir := os.Getenv(workloadClusterKubeConfigDir)
	if dir == "" {
		return fmt.Errorf("%s environment variable is not set", workloadClusterKubeConfigDir)
	}

	config.kubeConfigPath = path.Join(dir, o.ID+".kubeconfig")

	return nil
}

func Syncer() *syncer {
	return &syncer{
		// TODO: allow this to be set based on the KCP version exported by default
		image:     "ghcr.io/kcp-dev/kcp/syncer:release-0.4",
		namespace: "default",
		replicas:  1,
	}
}

type syncer struct {
	image           string
	namespace       string
	replicas        int
	resourcesToSync []string
}

func (s *syncer) Image(image string) *syncer {
	s.image = image
	return s
}

func (s *syncer) Namespace(namespace string) *syncer {
	s.namespace = namespace
	return s
}

func (s *syncer) Replicas(replicas int) *syncer {
	s.replicas = replicas
	return s
}

func (s *syncer) ResourcesToSync(resourcesToSync ...string) *syncer {
	s.resourcesToSync = resourcesToSync
	return s
}

func (s *syncer) applyTo(to interface{}) error {
	config, ok := to.(*workloadClusterConfig)
	if !ok {
		return fmt.Errorf("cannot apply WithKubeConfigByName to %q", to)
	}
	config.syncer = *s
	return nil
}

type workloadClusterConfig struct {
	name           string
	kubeConfigPath string
	workspace      *tenancyv1alpha1.ClusterWorkspace
	syncer         syncer
}

func createWorkloadCluster(t Test, name string, options ...Option) *workloadv1alpha1.WorkloadCluster {
	config := &workloadClusterConfig{
		name: name,
	}

	for _, option := range options {
		t.Expect(option.applyTo(config)).To(gomega.Succeed())
	}

	t.Expect(config.workspace).NotTo(gomega.BeNil())
	t.Expect(config.kubeConfigPath).NotTo(gomega.BeEmpty())

	// Run the KCP workload plugin sync command
	err := applyKcpWorkloadSync(t, config)
	t.Expect(err).NotTo(gomega.HaveOccurred())

	// Get the workload cluster and return it
	workloadCluster, err := t.Client().Kcp().Cluster(logicalcluster.From(config.workspace).Join(config.workspace.Name)).WorkloadV1alpha1().WorkloadClusters().Get(t.Ctx(), name, metav1.GetOptions{})
	t.Expect(err).NotTo(gomega.HaveOccurred())

	return workloadCluster
}

func applyKcpWorkloadSync(t Test, config *workloadClusterConfig) error {
	// Configure workload plugin kubeconfig for test workspace
	logicalClusterName := logicalcluster.From(config.workspace).Join(config.workspace.Name).String()
	clusterServer := fmt.Sprintf("%s/clusters/%s", t.Client().GetConfig().Host, logicalClusterName)
	syncCommandOutput := new(bytes.Buffer)
	opts := workloadplugin.NewOptions(genericclioptions.IOStreams{In: os.Stdin, Out: syncCommandOutput, ErrOut: os.Stderr})
	opts.KubectlOverrides.ClusterInfo.Server = clusterServer
	plugin, err := workloadplugin.NewConfig(opts)
	if err != nil {
		return err
	}

	// Run workload plugin sync command
	requiredResourcesToSync := sets.NewString("deployments.apps", "secrets", "configmaps", "serviceaccounts")
	userResourcesToSync := sets.NewString(config.syncer.resourcesToSync...)
	resourcesToSync := userResourcesToSync.Union(requiredResourcesToSync).List()
	err = plugin.Sync(t.Ctx(), config.name, config.syncer.namespace, config.syncer.image, resourcesToSync, config.syncer.replicas)
	if err != nil {
		return err
	}

	// Apply the syncer resources to the workload cluster
	clientConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: config.kubeConfigPath},
		&clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return err
	}

	client, err := dynamic.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	discoClient, err := discovery.NewDiscoveryClientForConfig(clientConfig)
	if err != nil {
		return err
	}
	cachedDiscoClient := memory.NewMemCacheClient(discoClient)
	restMapper := restmapper.NewDeferredDiscoveryRESTMapper(cachedDiscoClient)

	decoder := yaml.NewYAMLToJSONDecoder(bytes.NewReader(syncCommandOutput.Bytes()))

	for {
		resource := &unstructured.Unstructured{}
		err := decoder.Decode(resource)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		mapping, err := restMapper.RESTMapping(resource.GroupVersionKind().GroupKind(), resource.GroupVersionKind().Version)
		if err != nil {
			return err
		}
		_, err = client.Resource(mapping.Resource).Namespace(resource.GetNamespace()).Create(t.Ctx(), resource, metav1.CreateOptions{})
		if err != nil {
			if !errors.IsAlreadyExists(err) {
				return err
			}
			data, err := json.Marshal(resource)
			if err != nil {
				return err
			}
			_, err = client.Resource(mapping.Resource).Namespace(resource.GetNamespace()).Patch(t.Ctx(), resource.GetName(), types.ApplyPatchType, data, ApplyOptions.ToPatchOptions())
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func WorkloadCluster(t Test, workspace, name string) func(g gomega.Gomega) *workloadv1alpha1.WorkloadCluster {
	return func(g gomega.Gomega) *workloadv1alpha1.WorkloadCluster {
		c, err := t.Client().Kcp().Cluster(logicalcluster.New(workspace)).WorkloadV1alpha1().WorkloadClusters().Get(t.Ctx(), name, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return c
	}
}
