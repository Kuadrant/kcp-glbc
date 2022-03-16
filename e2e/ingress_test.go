//go:build e2e
// +build e2e

package e2e

import (
	"testing"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/cluster/v1alpha1"
	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
	tenancyhelper "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1/helper"
)

func TestIngress(t *testing.T) {
	test := With(t)

	test.WithNewTestWorkspace().
		Do(func(workspace *tenancyv1alpha1.Workspace) {
			cluster, err := NewWorkloadClusterWithKubeConfig("cluster1")
			test.Expect(err).NotTo(HaveOccurred())

			logicalCluster, err := tenancyhelper.EncodeLogicalClusterName(workspace)
			test.Expect(err).NotTo(HaveOccurred())

			cluster, err = test.Client().Kcp().Cluster(logicalCluster).ClusterV1alpha1().Clusters().Create(test.Ctx(), cluster, metav1.CreateOptions{})
			test.Expect(err).NotTo(HaveOccurred())

			test.Eventually(WorkloadCluster(test, cluster.ClusterName, cluster.Name)).Should(WithTransform(
				ConditionStatus(clusterv1alpha1.ClusterReadyCondition),
				Equal(corev1.ConditionTrue),
			))

			test.WithNewTestNamespace(InWorkspace(workspace), WithLabel("experimental.scheduling.kcp.dev/disabled", "")).
				Do(func(namespace *corev1.Namespace) {

				})
		})
}
