//go:build e2e
// +build e2e

package support

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"

	"github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kcp-dev/kcp/pkg/apis/cluster"
	clusterv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/cluster/v1alpha1"
)

func NewWorkloadClusterWithKubeConfig(name string) (*clusterv1alpha1.Cluster, error) {
	dir := os.Getenv(workloadClusterKubeConfigDir)
	if dir == "" {
		return nil, fmt.Errorf("%s environment variable is not set", workloadClusterKubeConfigDir)
	}

	data, err := ioutil.ReadFile(path.Join(dir, name+".yaml"))
	if err != nil {
		return nil, fmt.Errorf("error reading cluster %q Kubeconfig: %v", name, err)
	}

	workloadCluster := NewWorkloadCluster(name)
	workloadCluster.Spec.KubeConfig = string(data)

	return workloadCluster, nil
}

func NewWorkloadCluster(name string) *clusterv1alpha1.Cluster {
	return &clusterv1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cluster.GroupName,
			Kind:       "Cluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			// FIXME: workaround for https://github.com/kcp-dev/kcp/issues/730
			// Name: name,
			GenerateName: name + "-",
		},
	}
}

func WorkloadCluster(t Test, workspace, name string) func() *clusterv1alpha1.Cluster {
	return func() *clusterv1alpha1.Cluster {
		c, err := t.Client().Kcp().Cluster(workspace).ClusterV1alpha1().Clusters().Get(t.Ctx(), name, metav1.GetOptions{})
		t.Expect(err).NotTo(gomega.HaveOccurred())
		return c
	}
}
