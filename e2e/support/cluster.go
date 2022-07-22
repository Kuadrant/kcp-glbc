//go:build e2e

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

package support

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"

	workloadv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/workload/v1alpha1"
	workloadplugin "github.com/kcp-dev/kcp/pkg/cliplugins/workload/plugin"
	"github.com/kcp-dev/logicalcluster"
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
		image:     "ghcr.io/kcp-dev/kcp/syncer:cc96f19",
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
		return fmt.Errorf("cannot apply WithKubeConfigByName option to %q", to)
	}
	config.syncer = *s
	return nil
}

type workloadClusterConfig struct {
	name           string
	kubeConfigPath string
	workspace      logicalcluster.Name
	syncer         syncer
}

func createSyncTarget(t Test, name string, options ...Option) (*workloadv1alpha1.SyncTarget, func() error) {
	config := &workloadClusterConfig{
		name: name,
	}

	for _, option := range options {
		t.Expect(option.applyTo(config)).To(gomega.Succeed())
	}

	t.Expect(config.workspace.Empty()).NotTo(gomega.BeTrue())
	t.Expect(config.kubeConfigPath).NotTo(gomega.BeEmpty())

	// Run the KCP workload plugin sync command
	cleanup, err := applyKcpWorkloadSync(t, config)
	t.Expect(err).NotTo(gomega.HaveOccurred())

	// Get the workload cluster and return it
	syncTarget, err := t.Client().Kcp().Cluster(config.workspace).WorkloadV1alpha1().SyncTargets().Get(t.Ctx(), name, metav1.GetOptions{})
	t.Expect(err).NotTo(gomega.HaveOccurred())

	return syncTarget, cleanup
}

func deleteSyncTarget(t Test, syncTarget *workloadv1alpha1.SyncTarget) {
	// It's not possible to use foreground propagation policy as kcp doesn't currently support
	// garbage collection.
	propagationPolicy := metav1.DeletePropagationBackground
	err := t.Client().Kcp().Cluster(logicalcluster.From(syncTarget)).WorkloadV1alpha1().SyncTargets().Delete(t.Ctx(), syncTarget.Name, metav1.DeleteOptions{PropagationPolicy: &propagationPolicy})
	t.Expect(err).NotTo(gomega.HaveOccurred())
}

func applyKcpWorkloadSync(t Test, config *workloadClusterConfig) (func() error, error) {
	cleanup := func() error { return nil }

	// Configure workload plugin kubeconfig for test workspace
	clusterServer := t.Client().GetConfig().Host + config.workspace.Path()
	syncCommandOutput := new(bytes.Buffer)
	opts := workloadplugin.NewOptions(genericclioptions.IOStreams{In: os.Stdin, Out: syncCommandOutput, ErrOut: os.Stderr})
	opts.KubectlOverrides.ClusterInfo.Server = clusterServer
	plugin, err := workloadplugin.NewConfig(opts)
	if err != nil {
		return cleanup, err
	}

	// Run workload plugin sync command
	requiredResourcesToSync := sets.NewString("deployments.apps", "secrets", "configmaps", "serviceaccounts")
	userResourcesToSync := sets.NewString(config.syncer.resourcesToSync...)
	resourcesToSync := userResourcesToSync.Union(requiredResourcesToSync).List()
	err = plugin.Sync(t.Ctx(), "-", config.name, config.syncer.namespace, config.syncer.namespace, config.syncer.image, resourcesToSync, config.syncer.replicas, 0, 0)
	if err != nil {
		return cleanup, err
	}

	// Apply the syncer resources to the workload cluster
	clientConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: config.kubeConfigPath},
		&clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return cleanup, err
	}

	client, err := dynamic.NewForConfig(clientConfig)
	if err != nil {
		return cleanup, err
	}

	discoClient, err := discovery.NewDiscoveryClientForConfig(clientConfig)
	if err != nil {
		return cleanup, err
	}
	cachedDiscoClient := memory.NewMemCacheClient(discoClient)
	restMapper := restmapper.NewDeferredDiscoveryRESTMapper(cachedDiscoClient)

	decoder := yaml.NewYAMLToJSONDecoder(bytes.NewReader(syncCommandOutput.Bytes()))

	var resources []*unstructured.Unstructured

	cleanup = func() error {
		errs := make([]error, 0)
		// Iterate over the resources in reverse order
		for i := len(resources) - 1; i >= 0; i-- {
			resource := resources[i]
			mapping, err := restMapper.RESTMapping(resource.GroupVersionKind().GroupKind(), resource.GroupVersionKind().Version)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			propagationPolicy := metav1.DeletePropagationForeground
			err = client.Resource(mapping.Resource).Namespace(resource.GetNamespace()).Delete(t.Ctx(), resource.GetName(), metav1.DeleteOptions{PropagationPolicy: &propagationPolicy})
			if err != nil && !apierrors.IsNotFound(err) {
				errs = append(errs, err)
			}
		}
		return errors.NewAggregate(errs)
	}

	for {
		resource := &unstructured.Unstructured{}
		err := decoder.Decode(resource)
		if err == io.EOF {
			break
		}
		if err != nil {
			return cleanup, err
		}
		mapping, err := restMapper.RESTMapping(resource.GroupVersionKind().GroupKind(), resource.GroupVersionKind().Version)
		if err != nil {
			return cleanup, err
		}
		data, err := json.Marshal(resource)
		if err != nil {
			return cleanup, err
		}
		_, err = client.Resource(mapping.Resource).Namespace(resource.GetNamespace()).Patch(t.Ctx(), resource.GetName(), types.ApplyPatchType, data, ApplyOptions.ToPatchOptions())
		if err != nil {
			return cleanup, err
		}
		resources = append(resources, resource)
	}

	return cleanup, nil
}

func SyncTarget(t Test, workspace, name string) func(g gomega.Gomega) *workloadv1alpha1.SyncTarget {
	return func(g gomega.Gomega) *workloadv1alpha1.SyncTarget {
		c, err := t.Client().Kcp().Cluster(logicalcluster.New(workspace)).WorkloadV1alpha1().SyncTargets().Get(t.Ctx(), name, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return c
	}
}
